package netio

func split(p string) [][]byte {
	return splitBytes([]byte(p))
}

// routePattern reduces a registered path to the shape the router matches on, so
// the pattern a request reports having matched is the same string whether it
// was registered as "/v1/budget" or "/v1/budget/".
func routePattern(p string) string {
	for len(p) > 1 && p[len(p)-1] == '/' {
		p = p[:len(p)-1]
	}
	return p
}

func splitBytes(p []byte) [][]byte {
	// A trailing slash addresses no extra segment: "/v1/" and "/v1" name the
	// same resource. Trimming it before splitting is what keeps registration and
	// lookup on the same shape — otherwise a group registering Get("/") built
	// "/v1/", whose empty last segment no request for "/v1" could ever match.
	for len(p) > 1 && p[len(p)-1] == '/' {
		p = p[:len(p)-1]
	}

	if len(p) <= 1 {
		return nil
	}

	var res [][]byte
	start := 1

	for i := 1; i <= len(p); i++ {
		if i == len(p) || p[i] == '/' {
			res = append(res, p[start:i])
			start = i + 1
		}
	}

	return res
}
