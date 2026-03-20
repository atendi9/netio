package cors

import (
	"strconv"
	"strings"

	"github.com/atendi9/netio"
)

// Config defines the configuration for CORS middleware
type Config struct {
	AllowOrigins     []string // List of allowed origins, or ["*"] for all
	AllowMethods     []string // Allowed HTTP methods (GET, POST, PUT, DELETE, etc.)
	AllowHeaders     []string // Allowed request headers
	ExposeHeaders    []string // Headers exposed to the browser
	AllowCredentials bool     // Whether credentials (cookies/auth) are allowed
	MaxAge           int      // Cache duration in seconds
}

const AllowAll string = "*" // Special value to allow all origins in CORS

// Middleware returns a configurable CORS middleware
func Middleware(config Config) netio.Handler {
	allowMethods := strings.Join(config.AllowMethods, ", ")
	allowHeaders := strings.Join(config.AllowHeaders, ", ")
	exposeHeaders := strings.Join(config.ExposeHeaders, ", ")
	maxAge := ""
	if config.MaxAge > 0 {
		maxAge = strconv.Itoa(config.MaxAge)
	}

	return func(c *netio.Context) {
		origin := c.Header("Origin")
		allowed := false
		for _, o := range config.AllowOrigins {
			if o == AllowAll || o == origin {
				allowed = true
				break
			}
		}

		if allowed {
			c.HeaderSet("Access-Control-Allow-Origin", origin)
			if config.AllowCredentials {
				c.HeaderSet("Access-Control-Allow-Credentials", "true")
			}
			if exposeHeaders != "" {
				c.HeaderSet("Access-Control-Expose-Headers", exposeHeaders)
			}
		}

		if c.Method() == "OPTIONS" {
			if allowMethods != "" {
				c.HeaderSet("Access-Control-Allow-Methods", allowMethods)
			}
			if allowHeaders != "" {
				c.HeaderSet("Access-Control-Allow-Headers", allowHeaders)
			}
			if maxAge != "" {
				c.HeaderSet("Access-Control-Max-Age", maxAge)
			}
			c.SendStatus(204)
			c.Abort()
			return
		}

		c.Next()
	}
}
