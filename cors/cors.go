package cors

import (
	"slices"
	"strconv"
	"strings"

	"github.com/atendi9/netio"
)

// Config defines the configuration for CORS middleware
type Config struct {
	AllowOrigins     []string                 // List of allowed origins, or ["*"] for all
	AllowOriginFunc  func(origin string) bool // Allows for customized validation (Regex, DB, subdomains, etc.)
	AllowMethods     []string                 // Allowed HTTP methods (GET, POST, PUT, DELETE, etc.)
	AllowHeaders     []string                 // Allowed request headers
	ExposeHeaders    []string                 // Headers exposed to the browser
	AllowCredentials bool                     // Whether credentials (cookies/auth) are allowed
	MaxAge           int                      // Cache duration in seconds
}

const AllowAll string = "*" // Special value to allow all origins in CORS

// DefaultConfig returns a base configuration so you don't have to fill everything in manually.
func DefaultConfig() Config {
	return Config{
		AllowOrigins: []string{AllowAll},
		AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
	}
}

// Middleware returns a configurable CORS middleware
func Middleware(config Config) netio.Handler {
	allowAllOrigins := slices.Contains(config.AllowOrigins, AllowAll)

	allowMethods := strings.Join(config.AllowMethods, ", ")
	if allowMethods == "" {
		allowMethods = "GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS"
	}

	exposeHeaders := strings.Join(config.ExposeHeaders, ", ")
	allowAllHeaders := slices.Contains(config.AllowHeaders, AllowAll)
	allowHeadersStr := strings.Join(config.AllowHeaders, ", ")

	return func(c *netio.Context) {
		origin := c.Header("Origin")

		if origin == "" {
			c.Next()
			return
		}

		c.HeaderSet("Vary", "Origin")

		isAllowed := allowAllOrigins
		if !isAllowed {
			if config.AllowOriginFunc != nil {
				isAllowed = config.AllowOriginFunc(origin)
			} else {
				isAllowed = slices.Contains(config.AllowOrigins, origin)
			}
		}

		isPreflight := c.Method() == "OPTIONS"

		if !isAllowed {
			if isPreflight {
				c.SendStatus(204)
				c.Abort()
				return
			}
			c.Next()
			return
		}

		if config.AllowCredentials {
			c.HeaderSet("Access-Control-Allow-Origin", origin)
			c.HeaderSet("Access-Control-Allow-Credentials", "true")
		} else {
			if allowAllOrigins {
				c.HeaderSet("Access-Control-Allow-Origin", "*")
			} else {
				c.HeaderSet("Access-Control-Allow-Origin", origin)
			}
		}

		if exposeHeaders != "" {
			c.HeaderSet("Access-Control-Expose-Headers", exposeHeaders)
		}

		if isPreflight {
			c.HeaderSet("Access-Control-Allow-Methods", allowMethods)

			reqHeaders := c.Header("Access-Control-Request-Headers")
			if reqHeaders != "" {
				if allowAllHeaders {
					c.HeaderSet("Access-Control-Allow-Headers", reqHeaders)
				} else if len(config.AllowHeaders) > 0 {
					c.HeaderSet("Access-Control-Allow-Headers", allowHeadersStr)
				} else {
					c.HeaderSet("Access-Control-Allow-Headers", reqHeaders)
				}
			} else if len(config.AllowHeaders) > 0 {
				c.HeaderSet("Access-Control-Allow-Headers", allowHeadersStr)
			}

			if config.MaxAge > 0 {
				c.HeaderSet("Access-Control-Max-Age", strconv.Itoa(config.MaxAge))
			}

			c.SendStatus(204)
			c.Abort()
			return
		}

		c.Next()
	}
}
