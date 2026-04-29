export type FetchFn = typeof globalThis.fetch;

export function csrfFetch(inner: FetchFn): FetchFn {
  return (input, init) => {
    const request = new Request(input, init);
    const method = request.method.toUpperCase();
    if (method !== "GET" && method !== "HEAD") {
      if (!request.headers.has("Content-Type")) {
        const headers = new Headers(request.headers);
        headers.set("Content-Type", "application/json");
        return inner(new Request(request, { headers }));
      }
    }
    return inner(request);
  };
}
