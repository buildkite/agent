package pipeline

import (
	"github.com/buildkite/interpolate"
)

// Group is typically a key with no value. But it may also be a string
type Group interface {
	groupTag() // to distinguish from other types
	selfInterpolater
}

var _ Group = (*GroupString)(nil)

type GroupString string

func NewGroupString(s string) *GroupString {
	g := GroupString(s)
	return &g
}

func (*GroupString) groupTag() {}

func (g *GroupString) interpolate(env interpolate.Env) error {
	if g == nil {
		return nil
	}

	gInterp, err := interpolate.Interpolate(env, string(*g))
	if err != nil {
		return err
	}

	*g = GroupString(gInterp)

	return nil
}
