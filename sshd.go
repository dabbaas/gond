// Borrowed and modified from:
// https://github.com/jpillora/go-and-ssh/blob/master/sshd/server.go
// https://blog.gopheracademy.com/go-and-ssh/

package main

import (
    "encoding/base64"
    "encoding/binary"
    "fmt"
    "io"
    "io/ioutil"
    "log"
    "net"
    "os/exec"
    "sync"
    "syscall"
    "unsafe"

    "github.com/kr/pty"
    "golang.org/x/crypto/ssh"
)

func main() {

    config := &ssh.ServerConfig{
        PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
            uuid := c.User()
            pubKeyStr := base64.StdEncoding.EncodeToString(key.Marshal())
            return nil, verifyUUIDPubKey(uuid, pubKeyStr)
        },
    }

    // You can generate a keypair with 'ssh-keygen -t rsa'
    privateBytes, err := ioutil.ReadFile("id_rsa")
    if err != nil {
        log.Fatal("Failed to load private key (./id_rsa)")
    }

    private, err := ssh.ParsePrivateKey(privateBytes)
    if err != nil {
        log.Fatal("Failed to parse private key")
    }

    config.AddHostKey(private)

    // Once a ServerConfig has been configured, connections can be accepted.
    listener, err := net.Listen("tcp", "0.0.0.0:2200")
    if err != nil {
        log.Fatalf("Failed to listen on 2200 (%s)", err)
    }

    // Accept all connections
    log.Print("Listening on 2200...")
    for {
        tcpConn, err := listener.Accept()
        if err != nil {
            log.Printf("Failed to accept incoming connection (%s)", err)
            continue
        }
        // Before use, a handshake must be performed on the incoming net.Conn.
        sshConn, chans, reqs, err := ssh.NewServerConn(tcpConn, config)
        if err != nil {
            log.Printf("Failed to handshake (%s)", err)
            continue
        }

        log.Printf("New SSH connection from %s (%s)", sshConn.RemoteAddr(), sshConn.ClientVersion())

        // Discard all global out-of-band Requests
        go ssh.DiscardRequests(reqs)
        // Accept all channels
        go handleChannels(sshConn, chans)
    }
}

func handleChannels(sshConn *ssh.ServerConn, chans <-chan ssh.NewChannel) {
    // Service the incoming Channel channel in go routine
    for newChannel := range chans {
        go handleChannel(sshConn, newChannel)
    }
}

func handleChannel(sshConn *ssh.ServerConn, newChannel ssh.NewChannel) {
    // Since we're handling a shell, we expect a
    // channel type of "session". The also describes
    // "x11", "direct-tcpip" and "forwarded-tcpip"
    // channel types.
    if t := newChannel.ChannelType(); t != "session" {
        newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", t))
        return
    }

    // At this point, we have the opportunity to reject the client's
    // request for another logical connection
    connection, requests, err := newChannel.Accept()
    if err != nil {
        log.Printf("Could not accept channel (%s)", err)
        return
    }

    var bash *exec.Cmd
    bash, _ = createKubeCtlCmd(sshConn.User())

    // Prepare teardown function
    close := func() {
        connection.Close()
        _, err := bash.Process.Wait()
        if err != nil {
            log.Printf("Failed to exit bash (%s)", err)
        }
        log.Printf("Session closed")
    }

    // Allocate a terminal for this channel
    log.Print("Creating pty...")
    bashf, err := pty.Start(bash)
    if err != nil {
        log.Printf("Could not start pty (%s)", err)
        close()
        return
    }

    //pipe session to bash and visa-versa
    var once sync.Once
    go func() {
        io.Copy(connection, bashf)
        once.Do(close)
    }()
    go func() {
        io.Copy(bashf, connection)
        once.Do(close)
    }()

    // Sessions have out-of-band requests such as "shell", "pty-req" and "env"
    go func() {
        for req := range requests {
            log.Printf("%v", req.Type)
            switch req.Type {
            case "shell":
                // We only accept the default shell
                // (i.e. no command in the Payload)
                if len(req.Payload) == 0 {
                    req.Reply(true, nil)
                }
            case "pty-req":
                termLen := req.Payload[3]
                w, h := parseDims(req.Payload[termLen+4:])
                SetWinsize(bashf.Fd(), w, h)
                // Responding true (OK) here will let the client
                // know we have a pty ready for input
                req.Reply(true, nil)
            case "window-change":
                w, h := parseDims(req.Payload)
                SetWinsize(bashf.Fd(), w, h)
            }
        }
    }()
}

// =======================

// parseDims extracts terminal dimensions (width x height) from the provided buffer.
func parseDims(b []byte) (uint32, uint32) {
    w := binary.BigEndian.Uint32(b)
    h := binary.BigEndian.Uint32(b[4:])
    return w, h
}

// ======================

// Winsize stores the Height and Width of a terminal.
type Winsize struct {
    Height uint16
    Width  uint16
    x      uint16 // unused
    y      uint16 // unused
}

// SetWinsize sets the size of the given pty.
func SetWinsize(fd uintptr, w, h uint32) {
    ws := &Winsize{Width: uint16(w), Height: uint16(h)}
    syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCSWINSZ), uintptr(unsafe.Pointer(ws)))
}

// Borrowed from https://github.com/creack/termios/blob/master/win/win.go
