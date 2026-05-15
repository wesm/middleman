use anyhow::{Context, Result, anyhow, bail};
use base64::Engine;
use base64::engine::general_purpose::STANDARD as BASE64;
use chrono::{DateTime, Utc};
use portable_pty::{ChildKiller, CommandBuilder, PtySize, native_pty_system};
use rand::RngCore;
use serde::{Deserialize, Deserializer, Serialize, Serializer};
use sha2::{Digest, Sha256};
use std::collections::HashMap;
use std::env;
use std::fs::{self, OpenOptions};
use std::io::{self, BufRead, BufReader, Write};
use std::os::unix::fs::{OpenOptionsExt, PermissionsExt};
use std::os::unix::net::{UnixListener, UnixStream};
use std::path::{Path, PathBuf};
use std::sync::mpsc;
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;

const MAX_OUTPUT_REPLAY: usize = 64 * 1024;
const MAX_OWNER_FIRST_REQUEST_SIZE: usize = 8 * 1024;
const MAX_OWNER_REQUEST_SIZE: usize = 96 * 1024;
const MAX_OWNER_INPUT_SIZE: usize = 64 * 1024;
const MAX_UNIX_SOCKET_PATH_LEN: usize = 100;
const SUBSCRIBER_CHANNEL_CAPACITY: usize = 64;

#[derive(Debug, Default)]
struct Args {
    root: PathBuf,
    session: String,
    cwd: PathBuf,
    command: Vec<String>,
}

#[derive(Debug, Clone)]
struct SessionPaths {
    dir: PathBuf,
    socket: PathBuf,
    socket_dir: Option<PathBuf>,
    state_path: PathBuf,
}

struct OwnerCleanup {
    paths: SessionPaths,
    killer: Option<Box<dyn ChildKiller + Send + Sync>>,
}

struct ActiveAttachment {
    shared: Arc<Mutex<Shared>>,
    subscriber_id: Option<u64>,
}

#[derive(Clone)]
struct Subscriber {
    id: u64,
    tx: mpsc::SyncSender<Vec<u8>>,
}

#[derive(Debug, Serialize)]
struct OwnerState<'a> {
    session: &'a str,
    addr: String,
    token: &'a str,
    cwd: &'a str,
    pid: u32,
    created_at: DateTime<Utc>,
}

#[derive(Debug, Deserialize)]
struct Request {
    #[serde(rename = "type")]
    kind: String,
    #[serde(default)]
    token: String,
    #[serde(default)]
    cols: u16,
    #[serde(default)]
    rows: u16,
    #[serde(default, deserialize_with = "deserialize_base64_bytes")]
    data: Vec<u8>,
}

#[derive(Debug, Serialize)]
struct Response<'a> {
    #[serde(rename = "type")]
    kind: &'a str,
    #[serde(skip_serializing_if = "is_false")]
    ok: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    error: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    exit_code: Option<i32>,
    #[serde(
        skip_serializing_if = "Vec::is_empty",
        serialize_with = "serialize_base64_bytes"
    )]
    output: Vec<u8>,
}

#[derive(Default)]
struct Shared {
    output: Vec<u8>,
    subscribers: Vec<Subscriber>,
    next_subscriber_id: u64,
    attached: bool,
    exited: bool,
    reader_done: bool,
    exit_code: i32,
}

struct AttachRuntime {
    shared: Arc<Mutex<Shared>>,
    writer: Arc<Mutex<Box<dyn Write + Send>>>,
    master: Arc<Mutex<Box<dyn portable_pty::MasterPty + Send>>>,
}

fn main() {
    if let Err(err) = run() {
        eprintln!("{err:#}");
        std::process::exit(1);
    }
}

fn run() -> Result<()> {
    let args = parse_args(env::args().skip(1))?;
    run_owner(args)
}

fn parse_args<I>(args: I) -> Result<Args>
where
    I: IntoIterator<Item = String>,
{
    let mut values = HashMap::new();
    let mut iter = args.into_iter();
    while let Some(arg) = iter.next() {
        let key = match arg.as_str() {
            "-root" | "--root" => "root",
            "-session" | "--session" => "session",
            "-cwd" | "--cwd" => "cwd",
            "-command-json" | "--command-json" => "command-json",
            other => bail!("unknown argument {other}"),
        };
        let value = iter
            .next()
            .ok_or_else(|| anyhow!("argument {arg} requires a value"))?;
        values.insert(key.to_string(), value);
    }

    let root = values
        .remove("root")
        .ok_or_else(|| anyhow!("pty-manager root is required"))?;
    let session = values
        .remove("session")
        .ok_or_else(|| anyhow!("pty-manager session is required"))?;
    let cwd = values
        .remove("cwd")
        .ok_or_else(|| anyhow!("pty-manager cwd is required"))?;
    let command = match values.remove("command-json") {
        Some(raw) if !raw.is_empty() => serde_json::from_str(&raw).context("parse command-json")?,
        _ => default_shell_command(),
    };

    Ok(Args {
        root: PathBuf::from(root),
        session,
        cwd: PathBuf::from(cwd),
        command,
    })
}

fn run_owner(args: Args) -> Result<()> {
    validate_session_name(&args.session)?;
    if args.command.is_empty() {
        bail!("pty-manager command is empty");
    }

    let paths = session_paths(&args.root, &args.session)?;
    create_private_dir(&args.root).context("create pty manager root dir")?;
    create_private_dir(&paths.dir).context("create session state dir")?;
    if let Some(socket_dir) = &paths.socket_dir {
        create_private_dir(socket_dir).context("create fallback socket dir")?;
    }
    let _ = fs::remove_file(&paths.socket);
    let mut cleanup = OwnerCleanup::new(paths.clone());

    let listener = UnixListener::bind(&paths.socket)
        .with_context(|| format!("listen on {}", paths.socket.display()))?;
    listener.set_nonblocking(true)?;

    let pty_system = native_pty_system();
    let pair = pty_system
        .openpty(PtySize {
            rows: 24,
            cols: 80,
            pixel_width: 0,
            pixel_height: 0,
        })
        .context("open pty")?;

    let mut cmd = CommandBuilder::new(&args.command[0]);
    for arg in args.command.iter().skip(1) {
        cmd.arg(arg);
    }
    cmd.cwd(&args.cwd);
    strip_secret_env(&mut cmd);

    let mut child = pair.slave.spawn_command(cmd).context("spawn pty command")?;
    let killer = child.clone_killer();
    cleanup.set_killer(killer.clone_killer());
    let mut reader = pair.master.try_clone_reader().context("clone pty reader")?;
    let writer = Arc::new(Mutex::new(
        pair.master.take_writer().context("take pty writer")?,
    ));
    let master = Arc::new(Mutex::new(pair.master));
    drop(pair.slave);

    let token = new_token();
    let cwd_string = args.cwd.to_string_lossy().to_string();
    write_state(
        &paths,
        &OwnerState {
            session: &args.session,
            addr: format!("unix://{}", paths.socket.display()),
            token: &token,
            cwd: &cwd_string,
            pid: std::process::id(),
            created_at: Utc::now(),
        },
    )?;

    let shared = Arc::new(Mutex::new(Shared {
        exit_code: -1,
        ..Shared::default()
    }));

    let reader_shared = Arc::clone(&shared);
    thread::spawn(move || {
        let mut buf = [0_u8; 32 * 1024];
        loop {
            match reader.read(&mut buf) {
                Ok(0) | Err(_) => break,
                Ok(n) => broadcast(&reader_shared, &buf[..n]),
            }
        }
        mark_reader_done(&reader_shared);
    });

    let wait_shared = Arc::clone(&shared);
    thread::spawn(move || {
        let exit_code = match child.wait() {
            Ok(status) => status.exit_code() as i32,
            Err(_) => -1,
        };
        mark_child_exited(&wait_shared, exit_code);
    });

    loop {
        if owner_complete(&shared) {
            break;
        }
        let stream = match listener.accept() {
            Ok((stream, _addr)) => stream,
            Err(err) if err.kind() == io::ErrorKind::WouldBlock => {
                thread::sleep(Duration::from_millis(25));
                continue;
            }
            Err(err) => return Err(err).context("accept connection"),
        };
        stream.set_nonblocking(false)?;
        let token = token.clone();
        let conn_shared = Arc::clone(&shared);
        let writer = Arc::clone(&writer);
        let master = Arc::clone(&master);
        let mut killer = killer.clone_killer();
        thread::spawn(move || {
            let _ = handle_conn(stream, &token, conn_shared, writer, master, &mut killer);
        });
    }

    cleanup.disarm_killer();
    Ok(())
}

fn handle_conn(
    stream: UnixStream,
    token: &str,
    shared: Arc<Mutex<Shared>>,
    writer: Arc<Mutex<Box<dyn Write + Send>>>,
    master: Arc<Mutex<Box<dyn portable_pty::MasterPty + Send>>>,
    killer: &mut Box<dyn ChildKiller + Send + Sync>,
) -> Result<()> {
    let mut response_stream = stream.try_clone()?;
    let mut reader = BufReader::new(stream);
    let first = match read_request(&mut reader, MAX_OWNER_FIRST_REQUEST_SIZE)? {
        Some(req) => req,
        None => return Ok(()),
    };
    if first.token != token {
        write_response(
            &mut response_stream,
            Response {
                kind: "error",
                ok: false,
                error: Some("invalid pty owner token".to_string()),
                exit_code: None,
                output: Vec::new(),
            },
        )?;
        return Ok(());
    }

    match first.kind.as_str() {
        "status" => {
            let output = shared.lock().expect("shared poisoned").output.clone();
            write_response(&mut response_stream, ok_with_output(output))?;
        }
        "stop" => {
            write_response(&mut response_stream, ok())?;
            let _ = killer.kill();
        }
        "input" => {
            {
                let mut writer = writer.lock().expect("writer poisoned");
                writer.write_all(&first.data)?;
                writer.flush()?;
            }
            write_response(&mut response_stream, ok())?;
        }
        "resize" => {
            resize_pty(&master, first.cols, first.rows);
            write_response(&mut response_stream, ok())?;
        }
        "attach" => handle_attach(
            response_stream,
            reader,
            token,
            first,
            AttachRuntime {
                shared,
                writer,
                master,
            },
            killer,
        )?,
        _ => {
            write_response(
                &mut response_stream,
                Response {
                    kind: "error",
                    ok: false,
                    error: Some("unknown pty owner request".to_string()),
                    exit_code: None,
                    output: Vec::new(),
                },
            )?;
        }
    }
    Ok(())
}

fn handle_attach(
    mut stream: UnixStream,
    mut reader: BufReader<UnixStream>,
    token: &str,
    first: Request,
    runtime: AttachRuntime,
    killer: &mut Box<dyn ChildKiller + Send + Sync>,
) -> Result<()> {
    {
        let mut shared = runtime.shared.lock().expect("shared poisoned");
        if shared.attached {
            drop(shared);
            write_response(
                &mut stream,
                Response {
                    kind: "error",
                    ok: false,
                    error: Some("pty owner already has an active attachment".to_string()),
                    exit_code: None,
                    output: Vec::new(),
                },
            )?;
            return Ok(());
        }
        shared.attached = true;
    }
    let mut active_attachment = ActiveAttachment {
        shared: Arc::clone(&runtime.shared),
        subscriber_id: None,
    };

    resize_pty(&runtime.master, first.cols, first.rows);
    write_response(&mut stream, ok())?;

    let (tx, rx) = new_subscriber_channel();
    let (replay, already_complete, exit_code) = {
        let mut shared = runtime.shared.lock().expect("shared poisoned");
        let replay = shared.output.clone();
        let already_complete = shared.exited && shared.reader_done;
        let exit_code = shared.exit_code;
        let subscriber_id = if already_complete {
            None
        } else {
            Some(add_subscriber(&mut shared, tx.clone()))
        };
        if let Some(subscriber_id) = subscriber_id {
            active_attachment.subscriber_id = Some(subscriber_id);
        }
        (replay, already_complete, exit_code)
    };
    if !replay.is_empty() {
        if already_complete {
            write_response(&mut stream, ok_with_output(replay))?;
        } else {
            let _ = tx.send(replay);
        }
    }
    if already_complete {
        write_response(&mut stream, exit(exit_code))?;
        return Ok(());
    }
    drop(tx);

    let mut output_stream = stream.try_clone()?;
    let shared_for_output = Arc::clone(&runtime.shared);
    thread::spawn(move || {
        for chunk in rx {
            if write_response(&mut output_stream, ok_with_output(chunk)).is_err() {
                return;
            }
        }
        let code = shared_for_output.lock().expect("shared poisoned").exit_code;
        let _ = write_response(&mut output_stream, exit(code));
    });

    while let Some(req) = read_request(&mut reader, MAX_OWNER_REQUEST_SIZE)? {
        if req.token != token {
            return Ok(());
        }
        match req.kind.as_str() {
            "input" => {
                let mut writer = runtime.writer.lock().expect("writer poisoned");
                writer.write_all(&req.data)?;
                writer.flush()?;
            }
            "resize" if req.cols > 0 && req.rows > 0 => {
                resize_pty(&runtime.master, req.cols, req.rows);
            }
            "stop" => {
                let _ = killer.kill();
            }
            _ => {}
        }
    }
    Ok(())
}

fn resize_pty(master: &Arc<Mutex<Box<dyn portable_pty::MasterPty + Send>>>, cols: u16, rows: u16) {
    if cols > 0 && rows > 0 {
        let _ = master.lock().expect("master poisoned").resize(PtySize {
            cols,
            rows,
            pixel_width: 0,
            pixel_height: 0,
        });
    }
}

fn read_request<R: BufRead>(reader: &mut R, max_bytes: usize) -> Result<Option<Request>> {
    let mut line = Vec::new();
    loop {
        let available = reader.fill_buf()?;
        if available.is_empty() {
            if line.is_empty() {
                return Ok(None);
            }
            break;
        }

        let take = available
            .iter()
            .position(|byte| *byte == b'\n')
            .map_or(available.len(), |index| index + 1);
        if line.len() + take > max_bytes {
            bail!("pty owner request exceeds {max_bytes} bytes");
        }
        line.extend_from_slice(&available[..take]);
        reader.consume(take);
        if take > 0 && line.last() == Some(&b'\n') {
            break;
        }
    }

    let req: Request = serde_json::from_slice(&line)?;
    if req.kind == "input" && req.data.len() > MAX_OWNER_INPUT_SIZE {
        bail!("pty owner input exceeds {MAX_OWNER_INPUT_SIZE} bytes");
    }
    Ok(Some(req))
}

fn broadcast(shared: &Arc<Mutex<Shared>>, data: &[u8]) {
    let chunk = data.to_vec();
    let subscribers = {
        let mut shared = shared.lock().expect("shared poisoned");
        shared.output.extend_from_slice(data);
        if shared.output.len() > MAX_OUTPUT_REPLAY {
            let extra = shared.output.len() - MAX_OUTPUT_REPLAY;
            shared.output.drain(0..extra);
        }
        shared.subscribers.clone()
    };

    let mut failed_ids = Vec::new();
    for subscriber in subscribers {
        match subscriber.tx.try_send(chunk.clone()) {
            Ok(()) => {}
            Err(mpsc::TrySendError::Full(_)) | Err(mpsc::TrySendError::Disconnected(_)) => {
                failed_ids.push(subscriber.id);
            }
        }
    }
    if !failed_ids.is_empty() {
        let mut shared = shared.lock().expect("shared poisoned");
        shared
            .subscribers
            .retain(|subscriber| !failed_ids.contains(&subscriber.id));
    }
}

fn new_subscriber_channel() -> (mpsc::SyncSender<Vec<u8>>, mpsc::Receiver<Vec<u8>>) {
    mpsc::sync_channel(SUBSCRIBER_CHANNEL_CAPACITY)
}

fn add_subscriber(shared: &mut Shared, tx: mpsc::SyncSender<Vec<u8>>) -> u64 {
    let id = shared.next_subscriber_id;
    shared.next_subscriber_id += 1;
    shared.subscribers.push(Subscriber { id, tx });
    id
}

fn owner_complete(shared: &Arc<Mutex<Shared>>) -> bool {
    let shared = shared.lock().expect("shared poisoned");
    shared.exited && shared.reader_done
}

fn mark_child_exited(shared: &Arc<Mutex<Shared>>, exit_code: i32) {
    let subscribers = {
        let mut shared = shared.lock().expect("shared poisoned");
        shared.exit_code = exit_code;
        shared.exited = true;
        take_subscribers_if_complete(&mut shared)
    };
    drop(subscribers);
}

fn mark_reader_done(shared: &Arc<Mutex<Shared>>) {
    let subscribers = {
        let mut shared = shared.lock().expect("shared poisoned");
        shared.reader_done = true;
        take_subscribers_if_complete(&mut shared)
    };
    drop(subscribers);
}

fn take_subscribers_if_complete(shared: &mut Shared) -> Vec<Subscriber> {
    if shared.exited && shared.reader_done {
        std::mem::take(&mut shared.subscribers)
    } else {
        Vec::new()
    }
}

fn ok<'a>() -> Response<'a> {
    Response {
        kind: "ok",
        ok: true,
        error: None,
        exit_code: None,
        output: Vec::new(),
    }
}

fn ok_with_output<'a>(output: Vec<u8>) -> Response<'a> {
    Response {
        kind: "output",
        ok: true,
        error: None,
        exit_code: None,
        output,
    }
}

fn exit<'a>(exit_code: i32) -> Response<'a> {
    Response {
        kind: "exit",
        ok: true,
        error: None,
        exit_code: Some(exit_code),
        output: Vec::new(),
    }
}

fn write_response(stream: &mut UnixStream, response: Response<'_>) -> Result<()> {
    serde_json::to_writer(&mut *stream, &response)?;
    stream.write_all(b"\n")?;
    stream.flush()?;
    Ok(())
}

fn write_state(paths: &SessionPaths, state: &OwnerState<'_>) -> Result<()> {
    let data = serde_json::to_vec_pretty(state)?;
    let tmp = paths.state_path.with_extension("json.tmp");
    {
        let mut file = OpenOptions::new()
            .create(true)
            .truncate(true)
            .write(true)
            .mode(0o600)
            .open(&tmp)?;
        file.write_all(&data)?;
        file.sync_all()?;
    }
    fs::rename(tmp, &paths.state_path)?;
    fs::set_permissions(&paths.state_path, fs::Permissions::from_mode(0o600))?;
    Ok(())
}

fn create_private_dir(path: &Path) -> Result<()> {
    fs::create_dir_all(path)?;
    fs::set_permissions(path, fs::Permissions::from_mode(0o700))?;
    Ok(())
}

impl OwnerCleanup {
    fn new(paths: SessionPaths) -> Self {
        Self {
            paths,
            killer: None,
        }
    }

    fn set_killer(&mut self, killer: Box<dyn ChildKiller + Send + Sync>) {
        self.killer = Some(killer);
    }

    fn disarm_killer(&mut self) {
        self.killer = None;
    }
}

impl Drop for OwnerCleanup {
    fn drop(&mut self) {
        if let Some(killer) = &mut self.killer {
            let _ = killer.kill();
        }
        let _ = fs::remove_file(&self.paths.socket);
        if let Some(socket_dir) = &self.paths.socket_dir {
            let _ = fs::remove_dir_all(socket_dir);
        }
        let _ = fs::remove_dir_all(&self.paths.dir);
    }
}

impl Drop for ActiveAttachment {
    fn drop(&mut self) {
        let mut shared = self.shared.lock().expect("shared poisoned");
        if let Some(subscriber_id) = self.subscriber_id {
            shared
                .subscribers
                .retain(|subscriber| subscriber.id != subscriber_id);
        }
        shared.attached = false;
    }
}

fn session_paths(root: &Path, session: &str) -> Result<SessionPaths> {
    validate_session_name(session)?;
    let dir = root.join(session);
    let mut socket = root.join(format!("sock-{}", socket_hash(session)));
    let mut socket_dir = None;
    if socket.to_string_lossy().len() > MAX_UNIX_SOCKET_PATH_LEN {
        let fallback_dir = env::temp_dir().join(format!(
            "middleman-pty-{}",
            socket_hash(&format!("{}-{session}", root.display()))
        ));
        socket = fallback_dir.join("sock");
        socket_dir = Some(fallback_dir);
    }
    ensure_socket_path_fits(&socket)?;
    Ok(SessionPaths {
        state_path: dir.join("owner.json"),
        dir,
        socket,
        socket_dir,
    })
}

fn ensure_socket_path_fits(socket: &Path) -> Result<()> {
    if socket.to_string_lossy().len() > MAX_UNIX_SOCKET_PATH_LEN {
        bail!(
            "pty manager socket path is too long for Unix sockets: {}",
            socket.display()
        );
    }
    Ok(())
}

fn validate_session_name(session: &str) -> Result<()> {
    if session.is_empty()
        || session.contains("..")
        || session.contains('/')
        || session.contains('\\')
        || session.contains('\0')
    {
        bail!("unsafe pty owner session name {session:?}");
    }
    Ok(())
}

fn socket_hash(value: &str) -> String {
    let mut hasher = Sha256::new();
    hasher.update(value.as_bytes());
    let digest = hasher.finalize();
    digest[..8].iter().map(|b| format!("{b:02x}")).collect()
}

fn new_token() -> String {
    let mut data = [0_u8; 16];
    rand::rng().fill_bytes(&mut data);
    data.iter().map(|b| format!("{b:02x}")).collect()
}

fn default_shell_command() -> Vec<String> {
    env::var("SHELL")
        .map(|shell| vec![shell])
        .unwrap_or_else(|_| vec!["/bin/sh".to_string()])
}

fn strip_secret_env(cmd: &mut CommandBuilder) {
    for (key, _) in env::vars() {
        if key == "MIDDLEMAN_GITHUB_TOKEN"
            || key == "GITHUB_TOKEN"
            || key == "GH_TOKEN"
            || key == "GH_PAT"
            || key == "GITHUB_PAT"
            || key == "GITHUB_ENTERPRISE_TOKEN"
            || key == "GH_ENTERPRISE_TOKEN"
            || key.starts_with("MIDDLEMAN_GITHUB_TOKEN_")
            || key.starts_with("GITHUB_TOKEN_")
            || key.starts_with("GH_TOKEN_")
            || key.starts_with("GH_PAT_")
            || key.starts_with("GITHUB_PAT_")
            || key.starts_with("GITHUB_ENTERPRISE_TOKEN_")
            || key.starts_with("GH_ENTERPRISE_TOKEN_")
        {
            cmd.env_remove(key);
        }
    }
}

fn is_false(value: &bool) -> bool {
    !*value
}

fn serialize_base64_bytes<S>(bytes: &[u8], serializer: S) -> Result<S::Ok, S::Error>
where
    S: Serializer,
{
    serializer.serialize_str(&BASE64.encode(bytes))
}

fn deserialize_base64_bytes<'de, D>(deserializer: D) -> Result<Vec<u8>, D::Error>
where
    D: Deserializer<'de>,
{
    let value = Option::<String>::deserialize(deserializer)?;
    value
        .map(|data| BASE64.decode(data).map_err(serde::de::Error::custom))
        .transpose()
        .map(|data| data.unwrap_or_default())
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use std::io::Cursor;
    use std::sync::{
        Arc,
        atomic::{AtomicUsize, Ordering},
    };

    #[derive(Clone, Debug)]
    struct RecordingKiller {
        calls: Arc<AtomicUsize>,
    }

    impl ChildKiller for RecordingKiller {
        fn kill(&mut self) -> io::Result<()> {
            self.calls.fetch_add(1, Ordering::SeqCst);
            Ok(())
        }

        fn clone_killer(&self) -> Box<dyn ChildKiller + Send + Sync> {
            Box::new(self.clone())
        }
    }

    #[test]
    fn request_decodes_go_base64_bytes() {
        let req: Request = serde_json::from_value(json!({
            "type": "input",
            "token": "secret",
            "data": "aGVsbG8K"
        }))
        .unwrap();

        assert_eq!(req.kind, "input");
        assert_eq!(req.data, b"hello\n");
    }

    #[test]
    fn response_encodes_go_base64_bytes() {
        let raw = serde_json::to_value(ok_with_output(b"ready".to_vec())).unwrap();

        assert_eq!(raw["output"], "cmVhZHk=");
    }

    #[test]
    fn read_request_rejects_oversized_frames_before_decode() {
        let frame = format!(
            "{{\"type\":\"status\",\"token\":\"{}\"}}\n",
            "x".repeat(MAX_OWNER_FIRST_REQUEST_SIZE)
        );
        let mut reader = BufReader::new(Cursor::new(frame.into_bytes()));

        let err = read_request(&mut reader, MAX_OWNER_FIRST_REQUEST_SIZE).unwrap_err();

        assert!(err.to_string().contains("pty owner request exceeds"));
    }

    #[test]
    fn read_request_rejects_oversized_input_data() {
        let data = BASE64.encode(vec![b'x'; MAX_OWNER_INPUT_SIZE + 1]);
        let frame = format!("{{\"type\":\"input\",\"token\":\"secret\",\"data\":\"{data}\"}}\n");
        let mut reader = BufReader::new(Cursor::new(frame.into_bytes()));

        let err = read_request(&mut reader, MAX_OWNER_REQUEST_SIZE).unwrap_err();

        assert!(err.to_string().contains("pty owner input exceeds"));
    }

    #[test]
    fn paths_reject_unsafe_sessions() {
        for session in ["", "../ws", "a/b", "a\\b", "a\0b"] {
            assert!(session_paths(Path::new("/tmp/root"), session).is_err());
        }
    }

    #[test]
    fn paths_match_go_hash_shape() {
        let paths = session_paths(Path::new("/tmp/root"), "middleman-abc123").unwrap();

        assert_eq!(paths.dir, Path::new("/tmp/root/middleman-abc123"));
        assert_eq!(paths.socket, Path::new("/tmp/root/sock-cb190ac507b2b0a4"));
        assert!(paths.socket_dir.is_none());
        assert_eq!(
            paths.state_path,
            Path::new("/tmp/root/middleman-abc123/owner.json")
        );
    }

    #[test]
    fn paths_use_private_temp_socket_dir_for_long_roots() {
        let root = PathBuf::from("/tmp").join("x".repeat(MAX_UNIX_SOCKET_PATH_LEN));

        let paths = session_paths(&root, "middleman-abc123").unwrap();

        assert_eq!(paths.dir, root.join("middleman-abc123"));
        let socket_dir = paths.socket_dir.as_ref().unwrap();
        assert!(socket_dir.starts_with(env::temp_dir()));
        let expected_file_name = format!(
            "middleman-pty-{}",
            socket_hash(&format!("{}-middleman-abc123", root.display()))
        );
        assert_eq!(
            socket_dir.file_name().and_then(|name| name.to_str()),
            Some(expected_file_name.as_str())
        );
        assert_eq!(paths.socket, socket_dir.join("sock"));
        assert!(paths.socket.to_string_lossy().len() <= MAX_UNIX_SOCKET_PATH_LEN);
    }

    #[test]
    fn state_files_are_private() {
        let root = Path::new("/tmp").join(format!("mm-pty-test-{}", new_token()));
        let paths = session_paths(&root, "middleman-abc123").unwrap();
        create_private_dir(&paths.dir).unwrap();
        write_state(
            &paths,
            &OwnerState {
                session: "middleman-abc123",
                addr: "unix:///tmp/middleman.sock".to_string(),
                token: "secret",
                cwd: "/tmp/work",
                pid: 123,
                created_at: Utc::now(),
            },
        )
        .unwrap();

        let dir_mode = fs::metadata(&paths.dir).unwrap().permissions().mode() & 0o777;
        let state_mode = fs::metadata(&paths.state_path)
            .unwrap()
            .permissions()
            .mode()
            & 0o777;
        assert_eq!(dir_mode, 0o700);
        assert_eq!(state_mode, 0o600);

        fs::remove_dir_all(root).unwrap();
    }

    #[test]
    fn owner_cleanup_kills_child_and_removes_paths_on_drop() {
        let root = env::temp_dir().join(format!(
            "mm-pty-cleanup-test-{}-{}",
            new_token(),
            "x".repeat(MAX_UNIX_SOCKET_PATH_LEN)
        ));
        let paths = session_paths(&root, "middleman-abc123").unwrap();
        create_private_dir(&paths.dir).unwrap();
        if let Some(socket_dir) = &paths.socket_dir {
            create_private_dir(socket_dir).unwrap();
        }
        fs::write(&paths.socket, b"not a real socket").unwrap();

        let kill_calls = Arc::new(AtomicUsize::new(0));
        {
            let mut cleanup = OwnerCleanup::new(paths.clone());
            cleanup.set_killer(Box::new(RecordingKiller {
                calls: Arc::clone(&kill_calls),
            }));
        }

        assert_eq!(kill_calls.load(Ordering::SeqCst), 1);
        assert!(!paths.socket.exists());
        assert!(!paths.dir.exists());

        if root.exists() {
            fs::remove_dir_all(root).unwrap();
        }
    }

    #[test]
    fn child_exit_keeps_subscribers_open_until_reader_drains() {
        let (tx, rx) = mpsc::sync_channel(1);
        let shared = Arc::new(Mutex::new(Shared {
            subscribers: vec![Subscriber { id: 1, tx }],
            next_subscriber_id: 2,
            exit_code: -1,
            ..Shared::default()
        }));

        mark_child_exited(&shared, 7);
        assert!(rx.try_recv().is_err());

        broadcast(&shared, b"final");
        assert_eq!(rx.recv().unwrap(), b"final");

        mark_reader_done(&shared);
        assert!(rx.recv().is_err());
        assert_eq!(shared.lock().expect("shared poisoned").exit_code, 7);
    }

    #[test]
    fn broadcast_uses_bounded_subscriber_channels() {
        let (tx, rx) = new_subscriber_channel();
        let shared = Arc::new(Mutex::new(Shared {
            subscribers: vec![Subscriber { id: 1, tx }],
            next_subscriber_id: 2,
            exit_code: -1,
            ..Shared::default()
        }));

        broadcast(&shared, b"chunk");

        assert_eq!(rx.recv().unwrap(), b"chunk");
    }

    #[test]
    fn broadcast_removes_only_full_subscriber_channels() {
        let (full_tx, _full_rx) = new_subscriber_channel();
        for _ in 0..SUBSCRIBER_CHANNEL_CAPACITY {
            full_tx.try_send(vec![b'x']).unwrap();
        }
        let (active_tx, active_rx) = new_subscriber_channel();
        let shared = Arc::new(Mutex::new(Shared {
            subscribers: vec![
                Subscriber { id: 1, tx: full_tx },
                Subscriber {
                    id: 2,
                    tx: active_tx,
                },
            ],
            next_subscriber_id: 3,
            exit_code: -1,
            ..Shared::default()
        }));

        broadcast(&shared, b"chunk");

        assert_eq!(active_rx.recv().unwrap(), b"chunk");
        let remaining_ids: Vec<u64> = shared
            .lock()
            .expect("shared poisoned")
            .subscribers
            .iter()
            .map(|subscriber| subscriber.id)
            .collect();
        assert_eq!(remaining_ids, vec![2]);
    }
}
