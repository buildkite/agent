// +build !windows

package process

import (
	"errors"
	"os"
)

type processGroup struct{}

type ProcessTreeRoot struct {
	Children []*ProcessTreeNode
}

func (tree *ProcessTreeRoot) AddChild(child *ProcessTreeNode) error {
	return errors.New("Only valid on windows")
}

func (tree *ProcessTreeRoot) String() string {
	return ""
}

type ProcessTreeNode struct {
	Pid        int
	PPid       int
	Executable string
	Children   []*ProcessTreeNode
}

func (node *ProcessTreeNode) AddChild(child *ProcessTreeNode) error {
	return errors.New("Only valid on windows")
}

func (node *ProcessTreeNode) String(depth int) string {
	return ""
}

func newProcessGroup() (processGroup, error) {
	g := processGroup{}
	return g, errors.New("Only valid on windows")
}

func (g processGroup) dispose() error {
	return errors.New("Only valid on windows")
}

func (g processGroup) addProcess(p *os.Process) error {
	return errors.New("Only valid on windows")
}

func (g processGroup) processTree() (ProcessTreeRoot, error) {
	root := ProcessTreeRoot{}
	return root, errors.New("Only valid on windows")
}
