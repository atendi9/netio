package netio

func split(p string) [][]byte {
	if p == "/" {
		return nil
	}

	var res [][]byte
	start := 1

	for i := 1; i <= len(p); i++ {
		if i == len(p) || p[i] == '/' {
			res = append(res, []byte(p[start:i]))
			start = i + 1
		}
	}

	return res
}
