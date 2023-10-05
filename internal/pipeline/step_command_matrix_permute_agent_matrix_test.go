package pipeline

var agentBuildMatrixPermutations = []MatrixPermutation{
	{
		{Dimension: "os", Value: "dragonflybsd"},
		{Dimension: "arch", Value: "amd64"},
	},
	{
		{Dimension: "os", Value: "linux"},
		{Dimension: "arch", Value: "amd64"},
	},
	{
		{Dimension: "os", Value: "linux"},
		{Dimension: "arch", Value: "arm64"},
	},
	{
		{Dimension: "os", Value: "linux"},
		{Dimension: "arch", Value: "arm"},
	},
	{
		{Dimension: "os", Value: "linux"},
		{Dimension: "arch", Value: "armhf"},
	},
	{
		{Dimension: "os", Value: "linux"},
		{Dimension: "arch", Value: "ppc64"},
	},
	{
		{Dimension: "os", Value: "linux"},
		{Dimension: "arch", Value: "ppc64le"},
	},
	{
		{Dimension: "os", Value: "linux"},
		{Dimension: "arch", Value: "mips64le"},
	},
	{
		{Dimension: "os", Value: "linux"},
		{Dimension: "arch", Value: "386"},
	},
	{
		{Dimension: "os", Value: "linux"},
		{Dimension: "arch", Value: "s390x"},
	},
	{
		{Dimension: "os", Value: "netbsd"},
		{Dimension: "arch", Value: "amd64"},
	},
	{
		{Dimension: "os", Value: "freebsd"},
		{Dimension: "arch", Value: "386"},
	},
	{
		{Dimension: "os", Value: "freebsd"},
		{Dimension: "arch", Value: "amd64"},
	},
	{
		{Dimension: "os", Value: "openbsd"},
		{Dimension: "arch", Value: "386"},
	},
	{
		{Dimension: "os", Value: "openbsd"},
		{Dimension: "arch", Value: "amd64"},
	},
	{
		{Dimension: "os", Value: "windows"},
		{Dimension: "arch", Value: "386"},
	},
	{
		{Dimension: "os", Value: "windows"},
		{Dimension: "arch", Value: "amd64"},
	},
	{
		{Dimension: "os", Value: "windows"},
		{Dimension: "arch", Value: "arm64"},
	},
	{
		{Dimension: "os", Value: "darwin"},
		{Dimension: "arch", Value: "amd64"},
	},
	{
		{Dimension: "os", Value: "darwin"},
		{Dimension: "arch", Value: "arm64"},
	},
}
