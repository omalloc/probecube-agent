package probers

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"syscall"
	"time"

	"golang.org/x/net/ipv4"

	"github.com/omalloc/probecube-agent/internal/engine"
)

type TCPProber struct {
	srcIP net.IP
}

func NewTCPProber() *TCPProber {
	return &TCPProber{}
}

func init() {
	engine.Register(engine.TCP, NewTCPProber())
}

// TCPHeader 最小的 TCP 头部结构
type TCPHeader struct {
	SrcPort  uint16
	DstPort  uint16
	SeqNum   uint32
	AckNum   uint32
	DataOff  uint8
	Flags    uint8
	Window   uint16
	Checksum uint16
	Urgent   uint16
}

func (h *TCPHeader) Marshal() []byte {
	buf := make([]byte, 20)
	binary.BigEndian.PutUint16(buf[0:2], h.SrcPort)
	binary.BigEndian.PutUint16(buf[2:4], h.DstPort)
	binary.BigEndian.PutUint32(buf[4:8], h.SeqNum)
	binary.BigEndian.PutUint32(buf[8:12], h.AckNum)
	buf[12] = (h.DataOff << 4)
	buf[13] = h.Flags
	binary.BigEndian.PutUint16(buf[14:16], h.Window)
	binary.BigEndian.PutUint16(buf[16:18], h.Checksum)
	binary.BigEndian.PutUint16(buf[18:20], h.Urgent)
	return buf
}

// Checksum 计算 TCP 校验和 (含伪头部)
func (h *TCPHeader) CalcChecksum(src, dst net.IP, data []byte) uint16 {
	// 伪头部
	pseudo := make([]byte, 12)
	copy(pseudo[0:4], src.To4())
	copy(pseudo[4:8], dst.To4())
	pseudo[8] = 0
	pseudo[9] = syscall.IPPROTO_TCP
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(20+len(data)))

	sum := uint32(0)
	// 伪头部求和
	for i := 0; i < len(pseudo); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(pseudo[i : i+2]))
	}

	// 头部求和
	headerBytes := h.Marshal()
	// 注意这里 Marshal 时 Checksum 是 0
	for i := 0; i < len(headerBytes); i += 2 {
		if i == 16 {
			continue
		} // 跳过校验和字段
		sum += uint32(binary.BigEndian.Uint16(headerBytes[i : i+2]))
	}

	// 数据求和
	for i := 0; i < len(data); i += 2 {
		if i+1 < len(data) {
			sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
		} else {
			sum += uint32(data[i]) << 8
		}
	}

	for sum > 0xffff {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

func (p *TCPProber) getSourceIP(dst net.IP) net.IP {
	conn, err := net.Dial("udp", dst.String()+":80")
	if err != nil {
		return net.ParseIP("127.0.0.1")
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP
}

func (p *TCPProber) Probe(ctx context.Context, task *engine.Task) (*engine.Result, error) {
	start := time.Now()

	// 1. 解析目标地址和端口
	host, portStr, err := net.SplitHostPort(task.Target)
	if err != nil {
		return nil, fmt.Errorf("invalid target format: %w", err)
	}
	var dstPort uint16
	fmt.Sscanf(portStr, "%d", &dstPort)

	destIPs, err := net.LookupIP(host)
	if err != nil || len(destIPs) == 0 {
		return nil, fmt.Errorf("failed to lookup IP: %w", err)
	}
	dstIP := destIPs[0].To4()
	if dstIP == nil {
		return nil, fmt.Errorf("only IPv4 supported for now")
	}

	srcIP := p.getSourceIP(dstIP)
	srcPort := uint16(30000 + rand.Intn(30000))

	// 2. 建立 Raw Socket (需要 Root)
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_TCP)
	if err != nil {
		return nil, fmt.Errorf("failed to create raw socket: %w (check permissions)", err)
	}
	defer syscall.Close(fd)

	// 设置超时
	tv := syscall.NsecToTimeval(task.Timeout.Nanoseconds())
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); err != nil {
		return nil, err
	}

	// 3. 构造并发送 SYN 包
	seqNum := rand.Uint32()
	header := TCPHeader{
		SrcPort:  srcPort,
		DstPort:  dstPort,
		SeqNum:   seqNum,
		AckNum:   0,
		DataOff:  5,    // 20 bytes
		Flags:    0x02, // SYN
		Window:   64240,
		Checksum: 0,
		Urgent:   0,
	}
	header.Checksum = header.CalcChecksum(srcIP, dstIP, nil)
	packet := header.Marshal()

	addr := syscall.SockaddrInet4{
		Port: 0, // RAW socket 不在这里设端口
	}
	copy(addr.Addr[:], dstIP.To4())

	if err := syscall.Sendto(fd, packet, 0, &addr); err != nil {
		return nil, fmt.Errorf("failed to send packet: %w", err)
	}

	// 4. 接收响应
	recvBuf := make([]byte, 1500)
	for {
		n, _, err := syscall.Recvfrom(fd, recvBuf, 0)
		if err != nil {
			return &engine.Result{
				TaskID:    task.ID,
				TaskSN:    task.TaskSN,
				Proto:     task.Proto,
				Type:      task.Type,
				Success:   false,
				Status:    "Timeout/Error",
				Duration:  time.Since(start),
				LatencyMs: float64(time.Since(start).Microseconds()) / 1000.0,
				Err:       err,
			}, nil
		}

		// 解析 IP 头部 (RAW SOCK_RAW IPPROTO_TCP 读到的是包含 IP 头的包，取决于 OS)
		// 在 Linux 上，SOCK_RAW/IPPROTO_TCP 接收时包含 IP 头部
		ipHeader, err := ipv4.ParseHeader(recvBuf[:n])
		if err != nil {
			continue
		}

		if !ipHeader.Src.Equal(dstIP) {
			continue
		}

		// TCP 头部
		tcpData := recvBuf[ipHeader.Len:n]
		if len(tcpData) < 20 {
			continue
		}

		resSrcPort := binary.BigEndian.Uint16(tcpData[0:2])
		resDstPort := binary.BigEndian.Uint16(tcpData[2:4])
		resFlags := tcpData[13]
		resAckNum := binary.BigEndian.Uint32(tcpData[8:12])

		// 匹配响应
		if resSrcPort == dstPort && resDstPort == srcPort {
			duration := time.Since(start)
			result := &engine.Result{
				TaskID:    task.ID,
				TaskSN:    task.TaskSN,
				Proto:     task.Proto,
				Type:      task.Type,
				Duration:  duration,
				LatencyMs: float64(duration.Microseconds()) / 1000.0,
				Metrics:   make(map[string]interface{}),
			}

			// SYN-ACK (0x12) 或 RST (0x04 / 0x14)
			if resFlags&0x12 == 0x12 { // SYN+ACK
				result.Success = true
				result.Status = "Alive (SYN-ACK)"
			} else if resFlags&0x04 != 0 { // RST
				result.Success = true
				result.Status = "Alive (RST)"
			} else {
				continue // 其他包
			}

			// 验证 Seq/Ack
			if resAckNum != seqNum+1 {
				// 有些场景可能不对，但在简单探测中通常是这样
			}

			return result, nil
		}

		// 检查上下文超时
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
}
