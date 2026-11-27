//go:build windows

package process

func (p *Process) WinJobHandle() uintptr {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.winJobHandle
}
