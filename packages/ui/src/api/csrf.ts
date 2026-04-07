export type FetchFn = (
  input: Request,
) => Promise<Response>;

export function csrfFetch(inner: FetchFn): FetchFn {
  return (input: Request) => {
    const method = input.method.toUpperCase();
    if (method !== "GET" && method !== "HEAD") {
      if (!input.headers.has("Content-Type")) {
        const headers = new Headers(input.headers);
        headers.set("Content-Type", "application/json");
        return inner(new Request(input, { headers }));
      }
    }
    return inner(input);
  };
}
