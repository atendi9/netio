package cors

import (
	"slices"
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
	allowAllOrigins := slices.Contains(config.AllowOrigins, AllowAll)
	allowMethods := strings.Join(config.AllowMethods, ", ")
	if allowMethods == "" {
		allowMethods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"
	}

	exposeHeaders := strings.Join(config.ExposeHeaders, ", ")

	return func(c *netio.Context) {
		origin := c.Header("Origin")

		c.HeaderSet("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")

		allowed := false
		if allowAllOrigins {
			allowed = true
		} else {
			for _, o := range config.AllowOrigins {
				if o == origin {
					allowed = true
					break
				}
			}
		}

		if !allowed || origin == "" {
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
		if c.Method() == "OPTIONS" {
			c.HeaderSet("Access-Control-Allow-Methods", allowMethods)

			reqHeaders := c.Header("Access-Control-Request-Headers")

			if len(config.AllowHeaders) > 0 {
				if slices.Contains(config.AllowHeaders, AllowAll) {
					if reqHeaders != "" {
						c.HeaderSet("Access-Control-Allow-Headers", reqHeaders)
					}
				} else {
					c.HeaderSet("Access-Control-Allow-Headers", strings.Join(config.AllowHeaders, ", "))
				}
			} else if reqHeaders != "" {
				c.HeaderSet("Access-Control-Allow-Headers", reqHeaders)
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