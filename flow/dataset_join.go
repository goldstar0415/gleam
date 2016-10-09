package flow

import (
	"fmt"
	"io"
	"os"

	"github.com/chrislusf/gleam/util"
)

// Join joins two datasets by the key.
func (d *Dataset) Join(other *Dataset) *Dataset {
	sorted_d := d.Partition(len(d.Shards)).LocalSort()
	var sorted_other *Dataset
	if d == other {
		sorted_other = sorted_d
	} else {
		sorted_other = other.Partition(len(d.Shards)).LocalSort()
	}
	return sorted_d.JoinPartitionedSorted(sorted_other, false, false)
}

// Join multiple datasets that are sharded by the same key, and locally sorted within the shard
func (this *Dataset) JoinPartitionedSorted(that *Dataset,
	isLeftOuterJoin, isRightOuterJoin bool) *Dataset {
	ret := this.FlowContext.newNextDataset(len(this.Shards))

	inputs := []*Dataset{this, that}
	step := this.FlowContext.MergeDatasets1ShardTo1Step(inputs, ret)
	step.Name = "JoinPartitionedSorted"
	step.FunctionType = TypeJoinPartitionedSorted
	step.Function = func(task *Task) {
		outChan := task.OutputShards[0].IncomingChan

		leftReader := task.InputShards[0].OutgoingChans[0].Reader
		rightReader := task.InputShards[1].OutgoingChans[0].Reader
		if leftReader == rightReader {
			// special case for self join
			rightReader = task.InputShards[0].OutgoingChans[1].Reader
		}
		JoinPartitionedSorted(
			leftReader,
			rightReader,
			isLeftOuterJoin,
			isRightOuterJoin,
			outChan.Writer,
		)

		for _, shard := range task.OutputShards {
			shard.IncomingChan.Writer.Close()
		}
	}
	return ret
}

func JoinPartitionedSorted(leftRawChan, rightRawChan io.Reader, isLeftOuterJoin, isRightOuterJoin bool, outChan io.Writer) {
	leftChan := newChannelOfValuesWithSameKey(leftRawChan)
	rightChan := newChannelOfValuesWithSameKey(rightRawChan)

	// get first value from both channels
	leftValuesWithSameKey, leftHasValue := <-leftChan
	rightValuesWithSameKey, rightHasValue := <-rightChan

	for leftHasValue && rightHasValue {
		x := util.Compare(leftValuesWithSameKey.Key, rightValuesWithSameKey.Key)
		switch {
		case x == 0:
			// left and right cartician join
			for _, a := range leftValuesWithSameKey.Values {
				for _, b := range rightValuesWithSameKey.Values {
					t := []interface{}{leftValuesWithSameKey.Key}
					t = append(t, a.([]interface{})...)
					t = append(t, b.([]interface{})...)
					util.WriteRow(outChan, t...)
				}
			}
			leftValuesWithSameKey, leftHasValue = <-leftChan
			rightValuesWithSameKey, rightHasValue = <-rightChan
		case x < 0:
			if isLeftOuterJoin {
				for _, leftValue := range leftValuesWithSameKey.Values {
					t := []interface{}{leftValuesWithSameKey.Key}
					t = append(t, leftValue.([]interface{})...)
					util.WriteRow(outChan, t...)
				}
			}
			leftValuesWithSameKey, leftHasValue = <-leftChan
		case x > 0:
			if isRightOuterJoin {
				for _, rightValue := range rightValuesWithSameKey.Values {
					t := []interface{}{rightValuesWithSameKey.Key}
					t = append(t, rightValue.([]interface{})...)
					util.WriteRow(outChan, t...)
				}
			}
			rightValuesWithSameKey, rightHasValue = <-rightChan
		}
	}
	if leftHasValue {
		if isLeftOuterJoin {
			for _, leftValue := range leftValuesWithSameKey.Values {
				t := []interface{}{leftValuesWithSameKey.Key}
				t = append(t, leftValue.([]interface{})...)
				util.WriteRow(outChan, t...)
			}
		}
	}
	for leftValuesWithSameKey = range leftChan {
		if isLeftOuterJoin {
			for _, leftValue := range leftValuesWithSameKey.Values {
				t := []interface{}{leftValuesWithSameKey.Key}
				t = append(t, leftValue.([]interface{})...)
				util.WriteRow(outChan, t...)
			}
		}
	}
	if rightHasValue {
		if isRightOuterJoin {
			for _, rightValue := range rightValuesWithSameKey.Values {
				t := []interface{}{rightValuesWithSameKey.Key}
				t = append(t, rightValue.([]interface{})...)
				util.WriteRow(outChan, t...)
			}
		}
	}
	for rightValuesWithSameKey = range rightChan {
		if isRightOuterJoin {
			for _, rightValue := range rightValuesWithSameKey.Values {
				t := []interface{}{rightValuesWithSameKey.Key}
				t = append(t, rightValue.([]interface{})...)
				util.WriteRow(outChan, t...)
			}
		}
	}

}

type keyValues struct {
	Key    interface{}
	Values []interface{}
}

// create a channel to aggregate values of the same key
// automatically close original sorted channel
func newChannelOfValuesWithSameKey(sortedChan io.Reader) chan keyValues {
	outChan := make(chan keyValues, 1024)
	go func() {

		defer close(outChan)

		row, err := util.ReadRow(sortedChan)
		if err != nil {
			fmt.Fprintf(os.Stderr, "join read row error: %v", err)
			return
		}
		// fmt.Printf("join read len=%d, row: %s\n", len(row), row[0])

		keyValues := keyValues{
			Key:    row[0],
			Values: []interface{}{row[1:]},
		}
		for {
			row, err = util.ReadRow(sortedChan)
			if err != nil {
				outChan <- keyValues
				break
			}
			// fmt.Printf("join read len=%d, row: %s\n", len(row), row[0])
			x := util.Compare(keyValues.Key, row[0])
			if x == 0 {
				keyValues.Values = append(keyValues.Values, row[1:])
			} else {
				outChan <- keyValues
				keyValues.Key = row[0]
				keyValues.Values = []interface{}{row[1:]}
			}
		}
	}()

	return outChan
}
