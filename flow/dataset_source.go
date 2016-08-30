package flow

import (
	"bufio"
	"log"
	"os"
)

// Inputs: f(chan A), shardCount
func (fc *FlowContext) Source(f func(chan []byte)) (ret *Dataset) {
	ret = fc.newNextDataset(1)
	step := fc.AddOneToOneStep(nil, ret)
	step.Name = "Source"
	step.Function = func(task *Task) {
		// println("running source task...")
		for _, shard := range task.Outputs {
			f(shard.IncomingChan)
			close(shard.IncomingChan)
		}
	}
	return
}

func (fc *FlowContext) TextFile(fname string) (ret *Dataset) {
	fn := func(out chan []byte) {
		file, err := os.Open(fname)
		if err != nil {
			log.Panicf("Can not open file %s: %v", fname, err)
			return
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			b0 := scanner.Bytes()
			b1 := make([]byte, len(b0))
			copy(b1, b0)
			out <- b1
		}

		if err := scanner.Err(); err != nil {
			log.Printf("Scan file %s: %v", fname, err)
		}
	}
	return fc.Source(fn)
}

func (fc *FlowContext) Channel(ch chan []byte) (ret *Dataset) {
	ret = fc.newNextDataset(1)
	step := fc.AddOneToOneStep(nil, ret)
	step.Name = "Channel"
	step.Function = func(task *Task) {
		for data := range ch {
			task.Outputs[0].IncomingChan <- data
		}
		for _, shard := range task.Outputs {
			close(shard.IncomingChan)
		}
	}
	return
}

func (fc *FlowContext) Slice(slice [][]byte) (ret *Dataset) {
	inputChannel := make(chan []byte)

	go func() {
		for _, data := range slice {
			inputChannel <- data
		}
		close(inputChannel)
	}()

	return fc.Channel(inputChannel)
}
