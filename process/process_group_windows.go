// +build windows

package process

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"unsafe"

	ps "github.com/mitchellh/go-ps"
	"golang.org/x/sys/windows"
)

// We use this struct to retreive process handle(which is unexported)
// from os.Process using unsafe operation.
type handleRetreiver struct {
	Pid    int
	Handle uintptr
}

type processGroup windows.Handle

type ProcessTreeRoot struct {
	Children []*ProcessTreeNode
}

func (tree *ProcessTreeRoot) AddChild(child *ProcessTreeNode) error {
	tree.Children = append(tree.Children, child)
	return nil
}

func (tree *ProcessTreeRoot) String() string {
	result := ""
	for _, childNode := range tree.Children {
		result += childNode.String(1)
	}
	return result
}

type ProcessTreeNode struct {
	Pid        int
	PPid       int
	Executable string
	Children   []*ProcessTreeNode
}

func (node *ProcessTreeNode) AddChild(child *ProcessTreeNode) error {
	node.Children = append(node.Children, child)
	return nil
}

func (node *ProcessTreeNode) String(depth int) string {
	result := ""
	result += strings.Repeat(" ", depth*2) + fmt.Sprintf("%d - %s\n", node.Pid, node.Executable)
	for _, childNode := range node.Children {
		result += childNode.String(depth + 1)
	}
	return result
}

func newProcessGroup() (processGroup, error) {
	handle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}

	// this could be neat for enforcing clean up of all processes, but for
	// now we're only interested in improved observability
	//info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
	//	BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
	//		LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
	//	},
	//}
	//if _, err := windows.SetInformationJobObject(
	//	handle,
	//	windows.JobObjectExtendedLimitInformation,
	//	uintptr(unsafe.Pointer(&info)),
	//	uint32(unsafe.Sizeof(info))); err != nil {
	//	return 0, err
	//}

	return processGroup(handle), nil
}

func (g processGroup) dispose() error {
	return windows.CloseHandle(windows.Handle(g))
}

func (g processGroup) addProcess(p *os.Process) error {
	return windows.AssignProcessToJobObject(
		windows.Handle(g),
		windows.Handle((*handleRetreiver)(unsafe.Pointer(p)).Handle))
}

// this could be sent upstream to the windows package
type JOBOBJECT_BASIC_PROCESS_ID_LIST struct {
	NumberOfAssignedProcesses uint32
	NumberOfProcessIdsInList  uint32
	ProcessIdList             [1024]byte
}

func (g processGroup) listProcesses() ([]ps.Process, error) {
	JobObjectBasicProcessIdList := int32(3) // TODO upstream this to the windows package
	var list JOBOBJECT_BASIC_PROCESS_ID_LIST

	err := windows.QueryInformationJobObject(
		windows.Handle(g),
		JobObjectBasicProcessIdList,
		uintptr(unsafe.Pointer(&list)),
		uint32(unsafe.Sizeof(list)),
		nil,
	)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("QueryInformationJobObject failed: %v", err))
	}

	if list.NumberOfProcessIdsInList < list.NumberOfAssignedProcesses {
		return nil, errors.New(fmt.Sprintf("JOBOBJECT_BASIC_PROCESS_ID_LIST buffer not large enough. Got %d pids, wanted %d", list.NumberOfProcessIdsInList, list.NumberOfAssignedProcesses))
	}

	processes := make([]ps.Process, 0, list.NumberOfProcessIdsInList)
	for i := 0; i < int(list.NumberOfProcessIdsInList); i++ {
		pid := binary.LittleEndian.Uint64(list.ProcessIdList[8*i : 8*(i+1)])
		process, err := ps.FindProcess(int(pid))
		if process != nil && err == nil {
			processes = append(processes, process)
		}
	}
	return processes, nil
}

func (g processGroup) processTree() (ProcessTreeRoot, error) {
	root := ProcessTreeRoot{}
	processMap := make(map[int]*ProcessTreeNode)
	processes, err := g.listProcesses()

	if err != nil {
		return root, errors.New(fmt.Sprintf("error fetching process tree: %v", err))
	}

	for _, p := range processes {
		processMap[p.Pid()] = &ProcessTreeNode{
			Pid:        p.Pid(),
			PPid:       p.PPid(),
			Executable: p.Executable(),
		}
	}

	for _, node := range processMap {
		parentNode, parentExists := processMap[node.PPid]
		if parentExists {
			parentNode.AddChild(node)
		} else {
			root.AddChild(node)
		}
	}

	return root, nil
}
