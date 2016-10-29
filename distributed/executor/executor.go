package executor

import (
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/chrislusf/gleam/distributed/cmd"
	"github.com/chrislusf/gleam/distributed/netchan"
	"github.com/chrislusf/gleam/instruction"
	"github.com/chrislusf/gleam/util"
)

type ExecutorOption struct {
	Master       *string
	Host         *string
	Port         *int
	Dir          *string
	DataCenter   *string
	Rack         *string
	MaxExecutor  *int
	MemoryMB     *int64
	CPULevel     *int
	CleanRestart *bool
	HashCode     *uint32
}

type Executor struct {
	Option       *ExecutorOption
	Master       string
	instructions *cmd.InstructionSet
}

func NewExecutor(option *ExecutorOption, instructions *cmd.InstructionSet) *Executor {

	return &Executor{
		Option:       option,
		instructions: instructions,
	}
}

func (exe *Executor) ExecuteInstructionSet() {
	var wg sync.WaitGroup

	prevIsPipe := false
	prevOutputChan := util.NewPiper()
	for index, instruction := range exe.instructions.GetInstructions() {
		inputChan := prevOutputChan
		outputChan := util.NewPiper()
		wg.Add(1)
		go func(index int, instruction *cmd.Instruction, prevIsPipe bool, inChan, outChan *util.Piper) {
			exe.ExecuteInstruction(&wg, inChan, outChan,
				prevIsPipe,
				instruction,
				index == 0,
				index == len(exe.instructions.GetInstructions())-1,
				int(exe.instructions.GetReaderCount()),
			)
		}(index, instruction, prevIsPipe, inputChan, outputChan)
		prevOutputChan = outputChan
		if instruction.GetScript() != nil {
			prevIsPipe = instruction.GetScript().GetIsPipe()
		} else {
			prevIsPipe = false
		}
	}

	wg.Wait()
}

func connectInputOutput(wg *sync.WaitGroup, executorName string, inChan, outChan *util.Piper, i *cmd.Instruction, isFirst, isLast bool, readerCount int) {
	if isFirst && inChan != nil {
		wg.Add(1)
		inLocation := i.InputShardLocations[0]
		// println(executorName, "connecting to", inLocation.Address(), "to read", inLocation.GetName())
		go netchan.DialReadChannel(wg, executorName, inLocation.Address(), inLocation.GetName(), inChan.Writer)
	}
	if isLast && outChan != nil {
		wg.Add(1)
		outLocation := i.OutputShardLocations[0]
		// println(executorName, "connecting to", outLocation.Address(), "to write", outLocation.GetName())
		go netchan.DialWriteChannel(wg, executorName, outLocation.Address(), outLocation.GetName(), outChan.Reader, readerCount)
	}
}

func linkInReaders(wg *sync.WaitGroup, i *cmd.Instruction) (inChans []io.Reader) {
	for _, inputLocation := range i.GetInputShardLocations() {
		wg.Add(1)
		inChan := util.NewPiper()
		// println(i.GetName(), "connecting to", inputLocation.Address(), "to read", inputLocation.GetName())
		go netchan.DialReadChannel(wg, i.GetName(), inputLocation.Address(), inputLocation.GetName(), inChan.Writer)
		inChans = append(inChans, inChan.Reader)
	}
	return
}

func processWriters(wg *sync.WaitGroup, i *cmd.Instruction, fn func([]io.Writer)) {
	var outChans []*util.Piper
	for _, outputLocation := range i.GetOutputShardLocations() {
		wg.Add(1)
		outChan := util.NewPiper()
		go netchan.DialWriteChannel(wg, i.GetName(), outputLocation.Address(), outputLocation.GetName(), outChan.Reader, 1)
		outChans = append(outChans, outChan)
	}
	var writers []io.Writer
	for _, outChan := range outChans {
		writers = append(writers, outChan.Writer)
	}
	fn(writers)
	for _, outChan := range outChans {
		outChan.Writer.Close()
	}
}

// TODO: refactor this
func (exe *Executor) ExecuteInstruction(wg *sync.WaitGroup, inChan, outChan *util.Piper, prevIsPipe bool, i *cmd.Instruction, isFirst, isLast bool, readerCount int) {
	defer wg.Done()
	defer outChan.Writer.Close()

	// println("starting", *i.Name, "inChan", inChan, "outChan", outChan)
	if i.GetScript() != nil {
		connectInputOutput(wg, i.GetName(), inChan, outChan, i, isFirst, isLast, readerCount)

		command := exec.Command(
			i.GetScript().GetPath(), i.GetScript().GetArgs()...,
		)
		wg.Add(1)
		util.Execute(wg, i.GetName(), command, inChan.Reader, outChan.Writer, prevIsPipe, i.GetScript().GetIsPipe(), false, os.Stderr)

	} else if i.GetLocalSort() != nil {

		connectInputOutput(wg, i.GetName(), inChan, outChan, i, isFirst, isLast, readerCount)

		instruction.DoLocalSort(inChan.Reader, outChan.Writer, toOrderBys(i.GetLocalSort().GetOrderBys()))

	} else if i.GetPipeAsArgs() != nil {

		connectInputOutput(wg, i.GetName(), inChan, outChan, i, isFirst, isLast, readerCount)

		instruction.DoPipeAsArgs(inChan.Reader, outChan.Writer, i.GetPipeAsArgs().GetCode())

	} else if i.GetMergeSortedTo() != nil {

		connectInputOutput(wg, i.GetName(), nil, outChan, i, isFirst, isLast, readerCount)
		instruction.DoMergeSortedTo(linkInReaders(wg, i), outChan.Writer, toOrderBys(i.GetMergeSortedTo().GetOrderBys()))

	} else if i.GetScatterPartitions() != nil {

		connectInputOutput(wg, i.GetName(), inChan, nil, i, isFirst, isLast, readerCount)
		processWriters(wg, i, func(writers []io.Writer) {
			instruction.DoScatterPartitions(inChan.Reader, writers, toInts(i.GetScatterPartitions().GetIndexes()))
		})

	} else if i.GetRoundRobin() != nil {

		connectInputOutput(wg, i.GetName(), inChan, nil, i, isFirst, isLast, readerCount)
		processWriters(wg, i, func(writers []io.Writer) {
			instruction.DoRoundRobin(inChan.Reader, writers)
		})

	} else if i.GetCollectPartitions() != nil {

		connectInputOutput(wg, i.GetName(), nil, outChan, i, isFirst, isLast, readerCount)
		instruction.DoCollectPartitions(linkInReaders(wg, i), outChan.Writer)

	} else if i.GetInputSplitReader() != nil {

		connectInputOutput(wg, i.GetName(), inChan, outChan, i, isFirst, isLast, readerCount)

		instruction.DoInputSplitReader(inChan.Reader, outChan.Writer, i.GetInputSplitReader().GetInputType())

	} else if i.GetJoinPartitionedSorted() != nil {

		readers := linkInReaders(wg, i)
		jps := i.GetJoinPartitionedSorted()

		connectInputOutput(wg, i.GetName(), nil, outChan, i, isFirst, isLast, readerCount)
		instruction.DoJoinPartitionedSorted(readers[0], readers[1], outChan.Writer, toInts(i.GetJoinPartitionedSorted().GetIndexes()), *jps.IsLeftOuterJoin, *jps.IsRightOuterJoin)

	} else if i.GetCoGroupPartitionedSorted() != nil {

		readers := linkInReaders(wg, i)

		connectInputOutput(wg, i.GetName(), nil, outChan, i, isFirst, isLast, readerCount)
		instruction.DoCoGroupPartitionedSorted(readers[0], readers[1], outChan.Writer, toInts(i.GetCoGroupPartitionedSorted().GetIndexes()))

	} else if i.GetLocalTop() != nil {

		connectInputOutput(wg, i.GetName(), inChan, outChan, i, isFirst, isLast, readerCount)

		instruction.DoLocalTop(inChan.Reader, outChan.Writer, int(i.GetLocalTop().GetN()), toOrderBys(i.GetLocalTop().GetOrderBys()))

	} else if i.GetBroadcast() != nil {

		connectInputOutput(wg, i.GetName(), inChan, nil, i, isFirst, isLast, readerCount)
		processWriters(wg, i, func(writers []io.Writer) {
			instruction.DoBroadcast(inChan.Reader, writers)
		})

	} else if i.GetLocalHashAndJoinWith() != nil {

		readers := linkInReaders(wg, i)
		connectInputOutput(wg, i.GetName(), nil, outChan, i, isFirst, isLast, readerCount)
		instruction.DoLocalHashAndJoinWith(readers[0], readers[1], outChan.Writer, toInts(i.GetLocalHashAndJoinWith().GetIndexes()))

	} else {
		panic("what is this? " + i.String())
	}
}

func toInts(indexes []int32) []int {
	var ret []int
	for _, x := range indexes {
		ret = append(ret, int(x))
	}
	return ret
}

func toOrderBys(orderBys []*cmd.OrderBy) (ret []instruction.OrderBy) {
	for _, o := range orderBys {
		ret = append(ret, instruction.OrderBy{
			Index: int(o.GetIndex()),
			Order: instruction.Order(int(o.GetOrder())),
		})
	}
	return ret
}
