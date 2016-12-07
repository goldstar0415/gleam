package main

import (
	"os"
	"runtime/pprof"

	"github.com/chrislusf/gleam/distributed"
	"github.com/chrislusf/gleam/flow"
)

func main() {
	f, _ := os.Create("p.prof")
	pprof.StartCPUProfile(f)
	defer pprof.StopCPUProfile()

	bigFile := 2

	fileName := "/Users/chris/Desktop/record_10000_input.txt"
	partition := 2
	size := int64(1024)
	if bigFile == 1 {
		fileName = "/Users/chris/Desktop/record_1Gb_input.txt"
		partition = 4
		size = 1024
	}
	if bigFile == 2 {
		fileName = "/Users/chris/Desktop/record_10GB_input.txt"
		partition = 40
		size = 10240
	}

	gleamSortDistributed(fileName, size, partition)

}

func gleamSortStandalone(fileName string, partition int) {

	flow.New().TextFile(
		fileName,
	).Map(`
       function(line)
         return string.sub(line, 1, 10), string.sub(line, 13)
       end
   `).Partition(partition).Sort().Fprintf(os.Stdout, "%s  %s\n").Run()
}

func linuxSortDistributed(fileName string, partition int) {

	flow.New().TextFile(
		fileName,
	).Map(`
       function(line)
         return string.sub(line, 1, 10), string.sub(line, 13)
       end
    `).Partition(partition).Pipe(`
        sort -k 1
    `).MergeSortedTo(1).Fprintf(os.Stdout, "%s  %s\n").Run(distributed.Option())
}

func linuxSortStandalone(fileName string, partition int) {

	flow.New().TextFile(
		fileName,
	).Map(`
       function(line)
         return string.sub(line, 1, 10), string.sub(line, 13)
       end
    `).Partition(partition).Pipe(`
        sort -k 1
    `).MergeSortedTo(1).Fprintf(os.Stdout, "%s  %s\n").Run()
}

func gleamSortDistributed(fileName string, size int64, partition int) {

	f := flow.New().TextFile(
		fileName,
	).Hint(flow.TotalSize(size)).Map(`
       function(line)
         return string.sub(line, 1, 10), string.sub(line, 13)
       end
   `).OnDisk(func(d *flow.Dataset) *flow.Dataset {
		return d.Partition(partition).Sort()
	}).Fprintf(os.Stdout, "%s  %s\n")

	// f.Run(distributed.Planner())

	f.Run(distributed.Option())
}
