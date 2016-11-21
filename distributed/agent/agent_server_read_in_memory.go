package agent

import (
	"io"
	"log"
	"net"

	"github.com/chrislusf/gleam/util"
)

func (as *AgentServer) handleInMemoryReadConnection(conn net.Conn, readerName, channelName string) {

	log.Println(readerName, "waits in memory for", channelName)

	ch := as.inMemoryChannels.WaitForNamedDatasetShard(channelName)

	if ch == nil {
		log.Println(readerName, "in memory read an empty", channelName)
		return
	}

	log.Println(readerName, "start in memory reading", channelName)
	buf := make([]byte, util.BUFFER_SIZE)
	io.CopyBuffer(conn, ch.Reader, buf)

	log.Println(readerName, "finish in memory reading", channelName)
}
