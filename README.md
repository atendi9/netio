# NetIO – Lightweight HTTP Library for Go

<img src="./logo/netio.png" width=200>

NetIO is a fast, minimalistic HTTP library built on top of native TCP connections. It provides flexible routing, middleware support, automatic JSON parsing, and efficient context management – all without external dependencies.

## ✨ Features

- 🚀 High‑performance, zero‑dependency core
- 🛣️ Path parameter routing (e.g., `/users/:id`)
- 🔗 Global and per‑route middleware
- 📦 Automatic JSON body parsing & responses
- 🔍 Query string, header, and path parameter binding to structs
- 📁 Multipart file upload support
- 🌐 Built‑in CORS middleware
- ⚙️ Configurable maximum request body size
- 🧠 Optimised context pooling for low latency

## 📦 Installation

```bash
go get github.com/atendi9/netio
```

## 🚀 Quick Start

```go
package main

import (
    "encoding/json"
    "log"

    "github.com/atendi9/netio"
    "github.com/atendi9/netio/cors"
)

func main() {
    app, err := netio.New(netio.AppConfig{
        Port:        "8080",
        AppName:     "myapp",
        MaxBodySize: "10 MB",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Global middleware
    app.Use(cors.Middleware(cors.Config{
        AllowOrigins: []string{"*"},
    }))

    app.Use(func(c *netio.Context) {
        log.Printf("Method=%s Path=%s IP=%s", c.Method(), c.Path(), c.IP())
        c.Next()
    })

    // POST route
    app.POST("/", func(c *netio.Context) {
        body := c.Body()
        log.Println(string(body))
        c.JSON(map[string]any{"message": "Hello World"})
    })

    // GET route
    app.GET("/", func(c *netio.Context) {
        c.Send([]byte(`{"message":"Hello World"}`))
    })

    app.Listen()
}
```

## ⚙️ Configuration

`AppConfig` controls the server’s behaviour:

```go
type AppConfig struct {
    Port        string      // listening port (required)
    AppName     string      // application name for logs (optional)
    MaxBodySize MaxBodySize // max body size, e.g., "15 MB", "500 KB"
}
```

- `MaxBodySize` supports units: `B`, `KB`, `MB`, `GB`, `TB`. Default is `"15 MB"`.

## 🛣️ Routing

NetIO supports the common HTTP methods:

- `GET`
- `POST`
- `PUT`
- `DELETE`
- `PATCH`

### Path Parameters

Parameters are defined with a colon prefix:

```go
app.GET("/users/:id", func(c *netio.Context) {
    userID := c.Param("id")
    c.JSON(map[string]string{"user_id": userID})
})

app.GET("/users/:id/posts/:postId", func(c *netio.Context) {
    userID := c.Param("id")
    postID := c.Param("postId")
    c.JSON(map[string]string{
        "user_id": userID,
        "post_id": postID,
    })
})
```

## 🔧 Middleware

### Global Middleware

Add middleware that runs for every request:

```go
app.Use(func(c *netio.Context) {
    // before request
    log.Println("before")
    c.Next()
    // after request (if any)
    log.Println("after")
})
```

### CORS Middleware

Import the `cors` subpackage:

```go
import "github.com/atendi9/netio/cors"

app.Use(cors.Middleware(cors.Config{
    AllowOrigins:     []string{"http://localhost:3000"},
    AllowMethods:     []string{"GET", "POST", "PUT", "DELETE"},
    AllowHeaders:     []string{"Content-Type", "Authorization"},
    ExposeHeaders:    []string{"Content-Length"},
    AllowCredentials: true,
}))
```

## 📝 The Context Object

The `Context` holds all request/response data and provides helper methods.

### Request Data

```go
// HTTP method
method := c.Method()

// Path (with optional default)
path := c.Path() // or c.Path("/fallback")

// Headers
allHeaders := c.Headers()
userAgent := c.Header("User-Agent")

// Raw body
body := c.Body() // []byte

// Client IP
ip := c.IP()
ips := c.IPs() // X-Forwarded-For + direct IP

// Query parameters
name := c.Query("name", "default")
c.QueryParser(&struct {
    Name string `query:"name"`
    Age  int    `query:"age"`
}{})
```

### Path Parameters

```go
// Direct access
userID := c.Param("id")

// Parse into struct
type UserParams struct {
    UserID string `param:"id"`
    PostID string `param:"postId"`
}
var params UserParams
c.ParamsParser(&params)
```

### Request Body Parsing

```go
type User struct {
    Name  string `json:"name"`
    Email string `json:"email"`
}

var user User
if err := c.BodyParser(&user); err != nil {
    c.SendStatus(400)
    return
}
```

### Header Parsing

```go
type AuthHeaders struct {
    Authorization string `header:"Authorization"`
    XRequestID    string `header:"X-Request-ID"`
}

var headers AuthHeaders
c.ReqHeaderParser(&headers)
```

### Responses

```go
// Send JSON
c.JSON(map[string]string{"status": "ok"})

// Send raw bytes
c.Send([]byte("Hello"))

// Send only status code
c.SendStatus(204)

// Set response header
c.HeaderSet("X-Custom", "value")
```

### File Upload

```go
app.POST("/upload", func(c *netio.Context) {
    file, err := c.FormFile("file")
    if err != nil {
        c.SendStatus(400)
        return
    }

    // *multipart.FileHeader
    log.Printf("Received: %s (%d bytes)", file.Filename, file.Size)

    c.JSON(map[string]string{"status": "uploaded"})
})
```

## ⚖️ Body Size Limiting

Set the maximum allowed request body size in `AppConfig`. Requests exceeding the limit receive a `413 Payload Too Large` response.

```go
app, _ := netio.New(netio.AppConfig{
    Port:        "8080",
    MaxBodySize: "10 MB", // default is "15 MB"
})
```

## 📊 Logging

On startup, NetIO logs the server address:

```
myapp ▷ http.server is running
myapp ▷ http://localhost:8080
```

## 🎯 Advanced Examples

### Complete REST API

```go
type User struct {
    ID   string `json:"id"`
    Name string `json:"name"`
    Age  int    `json:"age"`
}

// GET /users/:id
app.GET("/users/:id", func(c *netio.Context) {
    id := c.Param("id")
    user := User{ID: id, Name: "John", Age: 30}
    c.JSON(user)
})

// POST /users
app.POST("/users", func(c *netio.Context) {
    var user User
    if err := c.BodyParser(&user); err != nil {
        c.SendStatus(400)
        return
    }
    // store user...
    c.SendStatus(201)
})

// PUT /users/:id
app.PUT("/users/:id", func(c *netio.Context) {
    id := c.Param("id")
    var user User
    if err := c.BodyParser(&user); err != nil {
        c.SendStatus(400)
        return
    }
    user.ID = id
    c.JSON(user)
})

// DELETE /users/:id
app.DELETE("/users/:id", func(c *netio.Context) {
    id := c.Param("id")
    // delete user...
    c.JSON(map[string]string{"deleted": id})
})
```

## 📄 License

MIT License – see [LICENSE](LICENSE) for details.

---