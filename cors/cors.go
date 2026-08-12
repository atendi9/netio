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
		AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "QUERY"},
	}
}

func resolveAllowedMethods(methods []string) string {
	if joined := strings.Join(methods, ", "); joined != "" {
		return joined
	}
	return "GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS, QUERY"
}

// normalizeList flattens comma-joined entries and drops blanks. Config values
// routinely come from a single environment variable holding a comma-separated
// list, which would otherwise arrive as one unusable element.
func normalizeList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, entry := range values {
		for v := range strings.SplitSeq(entry, ",") {
			if v = strings.TrimSpace(v); v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}

// normalizeOrigin canonicalizes an origin for comparison only. An origin is
// scheme://host[:port] with no path, and scheme and host are case-insensitive,
// so lowercasing and dropping a trailing slash cannot change which server it
// designates — it only stops a cosmetic mismatch from silently disabling CORS.
// The value echoed back to the browser is always the raw request Origin.
func normalizeOrigin(origin string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(origin), "/"))
}

func normalizeOrigins(origins []string) []string {
	out := normalizeList(origins)
	for i, o := range out {
		out[i] = normalizeOrigin(o)
	}
	return out
}

// Middleware returns a configurable CORS middleware
func Middleware(config Config) netio.Handler {
	normalizedHeaders := normalizeList(config.AllowHeaders)
	allowedOrigins := normalizeOrigins(config.AllowOrigins)
	headersConfigured := len(normalizedHeaders) > 0
	allowAllHeaders := slices.Contains(normalizedHeaders, AllowAll)
	allowedMethods := resolveAllowedMethods(normalizeList(config.AllowMethods))
	allowedHeaders := strings.Join(normalizedHeaders, ", ")
	exposedHeaders := strings.Join(normalizeList(config.ExposeHeaders), ", ")

	return func(c *netio.Context) {
		origin := c.Header("Origin")

		if origin == "" {
			c.Next()
			return
		}

		if !isOriginAllowed(origin, allowedOrigins, config.AllowOriginFunc) {
			// A rejected origin yields a response with no CORS headers, which
			// the browser reports only as a generic failure. Log it so the
			// cause is visible server-side instead of silent.
			c.Logger()("cors: origin ", origin, " is not allowed; configured origins: [", strings.Join(allowedOrigins, ", "), "]")
			c.Next()
			return
		}

		c.HeaderAppend("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")
		setOriginHeaders(c, origin, allowedOrigins, config.AllowCredentials)

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

// isOriginAllowed matches against the pre-normalized origin list. allowedOrigins
// is already canonical, so only the request origin needs normalizing here.
// AllowOriginFunc receives the raw origin: custom validation may be case- or
// suffix-sensitive in ways this package should not second-guess.
func isOriginAllowed(origin string, allowedOrigins []string, allowOriginFunc func(string) bool) bool {
	if slices.Contains(allowedOrigins, wildcard) || slices.Contains(allowedOrigins, normalizeOrigin(origin)) {
		return true
	}
	if allowOriginFunc != nil {
		return allowOriginFunc(origin)
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
