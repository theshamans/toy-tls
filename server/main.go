// socket-server project main.go
package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"safechat/common"
	"time"

	crypt "safechat/encryption"
)

// ConnState represents the state of the connection with the client.
type ConnState struct {
	clientHello bool
	priv        *crypt.PrivateKey
	sym         *[32]byte
}

func NewConnState() ConnState {
	return ConnState{
		clientHello: false,
		priv:        nil,
		sym:         nil,
	}
}

func (state *ConnState) setPrivKey(p crypt.PrivateKey) error {
	if state.priv != nil {
		return errors.New("private key was already set")
	}
	state.priv = &p
	return nil
}

func (state *ConnState) getPrivKey() crypt.PrivateKey {
	return *state.priv
}

func (state *ConnState) setSymKey(s [32]byte) error {
	if state.sym != nil {
		return errors.New("symmetric key was already set")
	}
	state.sym = &s
	return nil
}

func (state *ConnState) getSymKey() *[32]byte {
	return state.sym
}

// run is the main function of the server. It accepts client connections and processes their messages.
func run() error {
	fmt.Println("Server Running...")

	server, err := net.Listen(common.SERVER_TYPE, common.SERVER_BIND+":"+common.SERVER_PORT)
	if err != nil {
		fmt.Println("Error listening:", err.Error())
		return err
	}
	defer server.Close()

	fmt.Println("Listening on " + common.SERVER_HOST + ":" + common.SERVER_PORT)
	fmt.Println("Waiting for client...")

	for {
		connection, err := server.Accept()
		state := NewConnState()
		if err != nil {
			fmt.Println("Error accepting client: ", err.Error())
		}
		fmt.Println("client connected")
		processClient(connection, &state)
	}
}

func main() {
	// Running the code in a separate function allows executing the deferred
	// functions before exiting with code 1. The call os.Exit() stops the
	// subsequent deferred functions.
	err := run()
	if err != nil {
		fmt.Printf("An error occured: %s", err.Error())
		os.Exit(1)
	}
}

func processClient(connection net.Conn, state *ConnState) {

	defer func() {
		fmt.Println("client disconnected")
	}()

	for {
		err, isClosed := processMessage(connection, state)
		if err != nil {
			fmt.Printf("an error occured: %v", err)
			break
		}
		if isClosed {
			break
		}
	}
}

// processMessage processes a single message from the client and sets the state of the connection if in the middle of the handshake.
// The bool returned indicates whether the connection has been closed by the client.
func processMessage(connection net.Conn, state *ConnState) (error, bool) {
	buffer := make([]byte, 1024*1024)
	mLen, err := connection.Read(buffer)
	if err != nil {
		return err, false
	}
	if mLen == 0 {
		return errors.New("received null message"), false
	}
	header := buffer[0]
	content := buffer[1:mLen]

	switch header {
	case common.CLIENT_HELLO:
		fmt.Println("[client hello]: received client hello")
		pub, priv := crypt.GenerateKeyPair()
		err := state.setPrivKey(priv)
		if err != nil {
			connection.Write(compileMessage(common.ERROR, "client hello failed: received hello request twice"))
			fmt.Println("[server log] received hello request twice")
			break
		}

		pubBytes := pub.Marshal()
		sends := compileMessage(common.SERVER_HELLO, string(pubBytes))

		connection.Write(sends)

	case common.CLIENT_DONE:
		// At this step it is assumed that the client returned his symmetric
		// key.
		symKeyEncrypted := content
		fmt.Printf("[client done] received encrypted symmetric key: %s\n", string(symKeyEncrypted))

		privKey := state.getPrivKey()
		symKey := privKey.DecryptString(fmt.Sprintf("%s", symKeyEncrypted))
		fmt.Printf("[client done] decrypted symmetric key is: %v\n", symKey)

		symKey32 := [32]byte{}
		copy(symKey32[:], symKey[:])

		state.setSymKey(symKey32)

		time.Sleep(1 * time.Second)

		sends := compileMessage(common.SERVER_DONE, "")
		connection.Write(sends)

	case common.CLIENT_MSG:
		fmt.Printf("[message] received encrypted message: %s\n", base64.URLEncoding.EncodeToString(content))
		symkey := state.getSymKey()
		if symkey == nil {
			fmt.Println("error: client tried to send message without encryption")
			break
		}
		if len(content) == 0 {
			sends := []byte{common.ERROR}
			sends = append(sends, []byte("there is no point in encrypting null messages")...)
			connection.Write(sends)
			break
		}
		msg := crypt.DecryptAES(symkey[:], content)
		fmt.Printf("[message] decrypted message: %s\n", msg)

		sends := []byte{common.SERVER_MSG}
		sends = append(sends, content...)

		connection.Write(sends)
	case common.CLIENT_CLOSE:
		fmt.Println("[client close] received client close")
		connection.Write(compileMessage(common.SERVER_CLOSE, "bye bye!"))
		return nil, true
	default:
		fmt.Printf("[error] received invalid header")
		sends := compileMessage(common.ERROR, "received invalid header")
		connection.Write(sends)
	}
	return nil, false
}

// compileMessage writes a message to the client by appending the header and the body of the message
func compileMessage(header byte, body string) []byte {
	sends := []byte{header}
	if body != "" {
		sends = append(sends, []byte(body)...)
	}
	return sends
}
