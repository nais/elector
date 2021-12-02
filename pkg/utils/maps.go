package utils

func MergeStringMap(src, dst map[string]string) map[string]string {
	target := make(map[string]string)
	for k, v := range src {
		target[k] = v
	}
	for k, v := range dst {
		target[k] = v
	}
	return target
}

func MergeByteMap(src, dst map[string][]byte) map[string][]byte {
	target := make(map[string][]byte)
	for k, v := range src {
		target[k] = v
	}
	for k, v := range dst {
		target[k] = v
	}
	return target
}

func KeysFromStringMap(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k, _ := range m {
		keys = append(keys, k)
	}
	return keys
}

func KeysFromByteMap(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k, _ := range m {
		keys = append(keys, k)
	}
	return keys
}
