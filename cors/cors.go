package cors

import (
	"slices"
	"strconv"
	"strings"

	"github.com/atendi9/netio"
)

const wildcard = "*"

type Config struct {
	AllowOrigins     []string                 // List of allowed origins, or ["*"] for all
	AllowOriginFunc  func(origin string) bool // Allows for customized validation (Regex, DB, subdomains, etc.)
	AllowMethods     []string                 // Allowed HTTP methods (GET, POST, PUT, DELETE, etc.)
	AllowHeaders     []string                 // Allowed request headers
	ExposeHeaders    []string                 // Headers exposed to the browser
	AllowCredentials bool                     // Whether credentials (cookies/auth) are allowed
	MaxAge           int                      // Cache duration in seconds
}

// AllowAll is a special value to allow all origins in CORS
const AllowAll string = "*"

// DefaultConfig returns a base configuration so you don't have to fill everything in manually.
func DefaultConfig() Config {
	return Config{
		AllowOrigins: []string{AllowAll},
		AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
	}
}

func resolveAllowedMethods(methods []string) string {
	if joined := strings.Join(methods, ", "); joined != "" {
		return joined
	}
	return "GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS"
}

// Middleware returns a configurable CORS middleware
func Middleware(config Config) netio.Handler {
	headersConfigured := len(config.AllowHeaders) > 0
	allowAllHeaders := slices.Contains(config.AllowHeaders, AllowAll)
	allowedMethods := resolveAllowedMethods(config.AllowMethods)
	allowedHeaders := strings.Join(config.AllowHeaders, ", ")
	exposedHeaders := strings.Join(config.ExposeHeaders, ", ")

	return func(c *netio.Context) {
		origin := c.Header("Origin")

		if origin == "" || !isOriginAllowed(origin, config) {
			c.Next()
			return
		}

		c.HeaderAppend("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")
		setOriginHeaders(c, origin, config.AllowOrigins, config.AllowCredentials)

		if exposedHeaders != "" {
			c.HeaderSet("Access-Control-Expose-Headers", exposedHeaders)
		}

		if c.Method() == "OPTIONS" {
			handlePreflight(c, allowedMethods, allowedHeaders, allowAllHeaders, headersConfigured, config.MaxAge)
			return
		}

		c.Next()
	}
}

func isOriginAllowed(origin string, config Config) bool {
	if slices.Contains(config.AllowOrigins, wildcard) || slices.Contains(config.AllowOrigins, origin) {
		return true
	}
	if config.AllowOriginFunc != nil {
		return config.AllowOriginFunc(origin)
	}
	return false
}

func setOriginHeaders(c *netio.Context, origin string, allowedOrigins []string, allowCredentials bool) {
	allowAllOrigins := slices.Contains(allowedOrigins, wildcard)

	// A wildcard origin cannot be combined with credentials: echo the
	// request origin back instead so the browser accepts the response.
	if allowAllOrigins && !allowCredentials {
		c.HeaderSet("Access-Control-Allow-Origin", wildcard)
	} else {
		c.HeaderSet("Access-Control-Allow-Origin", origin)
	}

	if allowCredentials {
		c.HeaderSet("Access-Control-Allow-Credentials", "true")
	}
}

func handlePreflight(c *netio.Context, allowedMethods, allowedHeaders string, allowAllHeaders, headersConfigured bool, maxAge int) {
	c.HeaderSet("Access-Control-Allow-Methods", allowedMethods)

	if headers := resolveAllowedHeadersValue(c.Header("Access-Control-Request-Headers"), allowedHeaders, allowAllHeaders, headersConfigured); headers != "" {
		c.HeaderSet("Access-Control-Allow-Headers", headers)
	}

	if maxAge > 0 {
		c.HeaderSet("Access-Control-Max-Age", strconv.Itoa(maxAge))
	}

	c.SendStatus(204)
	c.Abort()
}

func resolveAllowedHeadersValue(reqHeaders, allowedHeaders string, allowAllHeaders, headersConfigured bool) string {
	if !headersConfigured || allowAllHeaders {
		return reqHeaders
	}
	return allowedHeaders
}
