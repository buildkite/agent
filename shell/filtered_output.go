package shell

import "io"

type FilteredOutput struct {
  Writer io.Writer
  Secrets *Environment
}

func (ow FilteredOutput) Write(p []byte) (n int, err error) {
  // TODO: Filter the bytes based on secrets
  return ow.Writer.Write(p)
}
