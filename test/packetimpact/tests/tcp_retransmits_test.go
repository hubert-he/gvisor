// Copyright 2020 The gVisor Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tcp_retransmits_test

import (
	"flag"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	tb "gvisor.dev/gvisor/test/packetimpact/testbench"
)

func init() {
	tb.RegisterFlags(flag.CommandLine)
}

// TestRetransmits tests retransmits occur at exponentially increasing
// time intervals.
func TestRetransmits(t *testing.T) {
	dut := tb.NewDUT(t)
	defer dut.TearDown()
	listenFd, remotePort := dut.CreateListener(unix.SOCK_STREAM, unix.IPPROTO_TCP, 1)
	defer dut.Close(listenFd)
	conn := tb.NewTCPIPv4(t, tb.TCP{DstPort: &remotePort}, tb.TCP{SrcPort: &remotePort})
	defer conn.Close()

	conn.Handshake()
	acceptFd, _ := dut.Accept(listenFd)
	defer dut.Close(acceptFd)

	dut.SetSockOptInt(acceptFd, unix.IPPROTO_TCP, unix.TCP_NODELAY, 1)

	sampleData := []byte("Sample Data")
	samplePayload := &tb.Payload{Bytes: sampleData}

	dut.Send(acceptFd, sampleData, 0)
	if _, err := conn.ExpectData(&tb.TCP{}, samplePayload, time.Second); err != nil {
		t.Fatalf("expected a packet with payload %v: %s", samplePayload, err)
	}
	// Give a chance for the dut to estimate RTO with RTT from the DATA-ACK.
	// TODO(gvisor.dev/issue/2685) Estimate RTO during handshake, after which
	// we can skip sending this ACK.
	conn.Send(tb.TCP{Flags: tb.Uint8(header.TCPFlagAck)})

	startRTO := time.Second
	current := startRTO
	first := time.Now()
	dut.Send(acceptFd, sampleData, 0)
	seq := tb.Uint32(uint32(*conn.RemoteSeqNum()))
	if _, err := conn.ExpectData(&tb.TCP{SeqNum: seq}, samplePayload, startRTO); err != nil {
		t.Fatalf("expected a packet with payload %v: %s", samplePayload, err)
	}
	// Expect retransmits of the same segment.
	for i := 0; i < 5; i++ {
		start := time.Now()
		if _, err := conn.ExpectData(&tb.TCP{SeqNum: seq}, samplePayload, 2*current); err != nil {
			t.Fatalf("expected a packet with payload %v: %s loop %d", samplePayload, err, i)
		}
		if i == 0 {
			startRTO = time.Now().Sub(first)
			current = 2 * startRTO
			continue
		}
		// Check if the probes came at exponentially increasing intervals.
		if p := time.Since(start); p < current-startRTO {
			t.Fatalf("retransmit came sooner interval %d probe %d\n", p, i)
		}
		current *= 2
	}
}
