package agent

import "fmt"

type Bootstrap struct {
}

func (b Bootstrap) Run() error {
	fmt.Printf("Lollies")

	return nil
}
