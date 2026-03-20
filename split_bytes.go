package netio

func splitBytes(p []byte) [][]byte {
	if len(p) == 1 {
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
