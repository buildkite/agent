package api

import "code.google.com/p/go-uuid/uuid"

func NewUUID() string {
	return uuid.New()
}
