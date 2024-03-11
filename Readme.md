# events-sdk-go [![go-doc](https://godoc.org/github.com/ht-sdks/events-sdk-go?status.svg)](https://godoc.org/github.com/ht-sdks/events-sdk-go)

Hightouch Events SDK for Go.

## Installation

The package can be installed via `go get`, we recommend that you use a
package version management system like the Go vendor directory or a tool like
Godep to avoid issues related to API breaking changes introduced between major
versions of the library.

To install it in the GOPATH:
```
go get https://github.com/ht-sdks/events-sdk-go
```

## Documentation

The links bellow should provide all the documentation needed to make the best
use of the library and the Hightouch Events API:

- [Documentation](https://hightouch.com/docs/events/sdks/go)
- [godoc](https://godoc.org/gopkg.in/ht-sdks/events-sdk-go.v3)
- [API](https://hightouch.com/docs/events/sdks/http)
- [Specs](https://hightouch.com/docs/events/event-spec)

## Usage

```go
package main

import (
  "os"

  "github.com/ht-sdks/events-sdk-go"
)

func main() {
  // Instantiates client to send events to the Hightouch Events API.
  client := analytics.New(os.Getenv("WRITE_KEY"))

  // Flushes any queued messages and closes the client.
  defer client.Close()

  // Enqueues a track event that will be sent asynchronously.
  client.Enqueue(analytics.Track{
    Event:  "Created Account",
    UserId: "123",
    Properties: analytics.Properties{
      "application": "Desktop",
      "version":     "1.2.3",
      "platform":    "osx",
    },
  })
}
```

## License

The library is released under the [MIT license](License.md).
