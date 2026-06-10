# sqidsd

sqidsd is a daemon that encodes and decodes [Sqids](https://sqids.org/) IDs via line-oriented JSON-RPC 2.0, over unix domain sockets and/or TCP sockets.

## Install

### go install

```console
$ go install github.com/fujiwara/sqidsd/cmd/sqidsd@latest
```

### mise

Install with [mise](https://mise.jdx.dev/) using the GitHub backend:

```console
$ mise use -g github:fujiwara/sqidsd
```

### Binary releases

Download from [GitHub Releases](https://github.com/fujiwara/sqidsd/releases).

## Usage

```
Usage: sqidsd <address> ... [flags]

A daemon to encode/decode Sqids IDs via line-oriented JSON-RPC 2.0.

Arguments:
  <address> ...    Addresses to listen on. A unix domain socket path (e.g.
                   /tmp/sqidsd.sock) or a TCP address (e.g. 127.0.0.1:8089) is
                   detected automatically ($SQIDSD_ADDRESS).

Flags:
  -h, --help                   Show context-sensitive help.
      --alphabet=STRING        Custom Sqids alphabet ($SQIDSD_ALPHABET).
      --min-length=0           Minimum length of generated Sqids IDs
                               ($SQIDSD_MIN_LENGTH).
      --shutdown-timeout=5s    Grace period to drain connections on shutdown
                               ($SQIDSD_SHUTDOWN_TIMEOUT).
      --version                Show version.
```

Listen addresses are given as positional arguments. An address in the `host:port` form is treated as a TCP address; anything else (a path containing `/`, or a bare file name) is treated as a unix domain socket path. Multiple addresses may be given to listen on them simultaneously.

```console
$ sqidsd /tmp/sqidsd.sock 127.0.0.1:8089
```

sqidsd shuts down gracefully on SIGINT/SIGTERM: it stops accepting new connections and requests, drains in-flight requests within `--shutdown-timeout`, and removes the unix socket file.

## Protocol

sqidsd speaks line-oriented [JSON-RPC 2.0](https://www.jsonrpc.org/specification):

- One JSON-RPC request per line, one response per line.
- Requests may be pipelined; responses are returned in request order.
- Notifications (requests without an `id` field) get no response.
- Batch requests (a JSON array on a line) are not supported.
- Lines longer than 1MiB are rejected with a parse error and the connection is closed.

### Methods

#### encode

Encodes an array of non-negative integers (up to uint64 range) into a Sqids ID string.

```json
{"jsonrpc":"2.0","id":1,"method":"encode","params":[1,2,3]}
{"jsonrpc":"2.0","result":"86Rf07","id":1}
```

#### decode

Decodes a Sqids ID string into an array of non-negative integers. `params` is an array containing exactly one string.

```json
{"jsonrpc":"2.0","id":2,"method":"decode","params":["86Rf07"]}
{"jsonrpc":"2.0","result":[1,2,3],"id":2}
```

### Examples

```console
$ echo '{"jsonrpc":"2.0","id":1,"method":"encode","params":[1,2,3]}' | nc -U -q1 /tmp/sqidsd.sock
{"jsonrpc":"2.0","result":"86Rf07","id":1}

$ echo '{"jsonrpc":"2.0","id":2,"method":"decode","params":["86Rf07"]}' | socat - TCP:127.0.0.1:8089
{"jsonrpc":"2.0","result":[1,2,3],"id":2}
```

### Error codes

| Code | Message | Condition |
|---|---|---|
| -32700 | Parse error | Invalid JSON, or the line is too long (the connection is closed) |
| -32600 | Invalid Request | `jsonrpc` is not "2.0", `method` is missing, or a batch request |
| -32601 | Method not found | Method other than `encode` / `decode` |
| -32602 | Invalid params | Wrong params shape, negative/fractional/out-of-range numbers |
| -32000 | Server error | Encoding failed, or decoding an invalid Sqids string |

```json
{"jsonrpc":"2.0","id":3,"method":"decode","params":["!!!"]}
{"jsonrpc":"2.0","error":{"code":-32000,"message":"Server error","data":"invalid sqids string"},"id":3}
```

## LICENSE

MIT License

## Author

Copyright (c) 2023 FUJIWARA Shunichiro
