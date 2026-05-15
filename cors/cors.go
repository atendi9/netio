package cors

import (
	"slices"
	"strconv"
	"strings"

	"github.com/atendi9/netio"
)

const wildcard = "*"

type Config struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials bool
	MaxAgeSeconds  int
}

func resolveAllowedMethods(methods []string) string {
	if joined := strings.Join(methods, ", "); joined != "" {
		return joined
	}
	return "GET, POST, PUT, PATCH, DELETE, OPTIONS"
}

func Middleware(config Config) netio.Handler {
	headersConfigured := len(config.AllowedHeaders) > 0
	allowAllHeaders := slices.Contains(config.AllowedHeaders, wildcard)
	allowedMethods := resolveAllowedMethods(config.AllowedMethods)
	allowedHeaders := strings.Join(config.AllowedHeaders, ", ")
	exposedHeaders := strings.Join(config.ExposedHeaders, ", ")

	return func(c *netio.Context) {
		origin := c.Header("Origin")

		if origin == "" || !isOriginAllowed(origin, config.AllowedOrigins) {
			c.Next()
			return
		}

		c.HeaderAppend("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")
		setOriginHeaders(c, origin, config.AllowedOrigins, config.AllowCredentials)

		if exposedHeaders != "" {
			c.HeaderSet("Access-Control-Expose-Headers", exposedHeaders)
		}

		if c.Method() == "OPTIONS" {
			handlePreflight(c, allowedMethods, allowedHeaders, allowAllHeaders, headersConfigured, config.MaxAgeSeconds)
			return
		}

		c.Next()
	}
}

func isOriginAllowed(origin string, allowedOrigins []string) bool {
	return slices.Contains(allowedOrigins, wildcard) || slices.Contains(allowedOrigins, origin)
}

func setOriginHeaders(c *netio.Context, origin string, allowedOrigins []string, allowCredentials bool) {
	allowAllOrigins := slices.Contains(allowedOrigins, wildcard)

	if allowAllOrigins && !allowCredentials {
		c.HeaderSet("Access-Control-Allow-Origin", wildcard)
	} else {
		c.HeaderSet("Access-Control-Allow-Origin", origin)
	}

	if allowCredentials {
		c.HeaderSet("Access-Control-Allow-Credentials", "true")
	}
}

func handlePreflight(c *netio.Context, allowedMethods, allowedHeaders string, allowAllHeaders, headersConfigured bool, MaxAgeSeconds int) {
	c.HeaderSet("Access-Control-Allow-Methods", allowedMethods)

	if headers := resolveAllowedHeadersValue(c.Header("Access-Control-Request-Headers"), allowedHeaders, allowAllHeaders, headersConfigured); headers != "" {
		c.HeaderSet("Access-Control-Allow-Headers", headers)
	}

	if MaxAgeSeconds > 0 {
		c.HeaderSet("Access-Control-Max-Age", strconv.Itoa(MaxAgeSeconds))
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
