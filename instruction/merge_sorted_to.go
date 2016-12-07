package instruction

import (
	"io"
	"log"

	"github.com/chrislusf/gleam/msg"
	"github.com/chrislusf/gleam/util"
	"github.com/golang/protobuf/proto"
)

func init() {
	InstructionRunner.Register(func(m *msg.Instruction) Instruction {
		if m.GetMergeSortedTo() != nil {
			return NewMergeSortedTo(
				toOrderBys(m.GetMergeSortedTo().GetOrderBys()),
			)
		}
		return nil
	})
}

type MergeSortedTo struct {
	orderBys []OrderBy
}

func NewMergeSortedTo(orderBys []OrderBy) *MergeSortedTo {
	return &MergeSortedTo{orderBys}
}

func (b *MergeSortedTo) Name() string {
	return "MergeSortedTo"
}

func (b *MergeSortedTo) Function() func(readers []io.Reader, writers []io.Writer, stats *Stats) error {
	return func(readers []io.Reader, writers []io.Writer, stats *Stats) error {
		return DoMergeSortedTo(readers, writers[0], b.orderBys)
	}
}

func (b *MergeSortedTo) SerializeToCommand() *msg.Instruction {
	return &msg.Instruction{
		Name: proto.String(b.Name()),
		MergeSortedTo: &msg.MergeSortedTo{
			OrderBys: getOrderBys(b.orderBys),
		},
	}
}

func (b *MergeSortedTo) GetMemoryCostInMB(partitionSize int64) int64 {
	return 5
}

// Top streamingly compare and get the top n items
func DoMergeSortedTo(readers []io.Reader, writer io.Writer, orderBys []OrderBy) error {
	indexes := getIndexesFromOrderBys(orderBys)

	pq := newMinQueueOfPairs(orderBys)

	// enqueue one item to the pq from each channel
	for shardId, reader := range readers {
		if x, err := util.ReadMessage(reader); err == nil {
			if keys, err := util.DecodeRowKeys(x, indexes); err != nil {
				log.Printf("%v: %+v", err, x)
				return err
			} else {
				pq.Enqueue(pair{keys: keys, data: x}, shardId)
			}
		} else {
			if err != io.EOF {
				log.Printf("DoMergeSortedTo failed start :%v")
				return err
			}
		}
	}
	for pq.Len() > 0 {
		t, shardId := pq.Dequeue()
		if err := util.WriteMessage(writer, t.(pair).data); err != nil {
			return err
		}
		if x, err := util.ReadMessage(readers[shardId]); err == nil {
			if keys, err := util.DecodeRowKeys(x, indexes); err != nil {
				log.Printf("%v: %+v", err, x)
			} else {
				pq.Enqueue(pair{keys: keys, data: x}, shardId)
			}
		} else {
			if err != io.EOF {
				log.Printf("DoMergeSortedTo failed to ReadMessage :%v", err)
				return err
			}
		}
	}
	return nil
}
