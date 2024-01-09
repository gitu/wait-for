# wait-for
Wait for is a small utility that will wait for a given host and port to be available.

## Usage
Just run `wait-for` with the host and port you want to wait for.

```bash
  wait-for -u http://localhost:8080 -p google.com:443
```

You can specify either a uri to execute a GET request against or a host and port to check for TCP connectivity.
Per default the GET request will be executed until a 200 OK is returned. You can specify a different status code by prefixing it with `:`.

```bash
  wait-for -u 204:google.com:443
```

If the requests are not successful before the global timeout is reached, the program will exit with a non-zero exit code.

```bash
  wait-for -u http://localhost:8080 -p google.com:443 -t 10s
```

## Example
![demo of wait-for](demo.gif)

