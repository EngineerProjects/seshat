package common

type SubagentToolState struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Msg    string `json:"msg,omitempty"`
}

func Clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func ClampInt(v, lo, hi int) int {
	return Clamp(v, lo, hi)
}

func ShortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
