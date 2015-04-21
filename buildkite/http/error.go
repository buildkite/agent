package http

type Error struct {
	Status string
}

func (e Error) Error() string {
	return e.Status
}
