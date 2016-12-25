package instruction

import (
	"io"

	"github.com/chrislusf/gleam/pb"
	"github.com/chrislusf/gleam/util"
)

func init() {
	InstructionRunner.Register(func(m *pb.Instruction) Instruction {
		if m.GetCollectPartitions() != nil {
			return NewCollectPartitions()
		}
		return nil
	})
}

type CollectPartitions struct {
}

func NewCollectPartitions() *CollectPartitions {
	return &CollectPartitions{}
}

func (b *CollectPartitions) Name() string {
	return "CollectPartitions"
}

func (b *CollectPartitions) Function() func(readers []io.Reader, writers []io.Writer, stats *Stats) error {
	return func(readers []io.Reader, writers []io.Writer, stats *Stats) error {
		return DoCollectPartitions(readers, writers[0])
	}
}

func (b *CollectPartitions) SerializeToCommand() *pb.Instruction {
	return &pb.Instruction{
		Name:              b.Name(),
		CollectPartitions: &pb.CollectPartitions{},
	}
}

func (b *CollectPartitions) GetMemoryCostInMB(partitionSize int64) int64 {
	return 3
}

func DoCollectPartitions(readers []io.Reader, writer io.Writer) error {

	if len(readers) == 1 {
		_, err := io.Copy(writer, readers[0])
		return err
	}

	return util.CopyMultipleReaders(readers, writer)
}
