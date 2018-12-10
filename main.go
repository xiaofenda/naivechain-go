package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gorilla/websocket"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type MessageType int

type Block struct {
	Index        int
	PreviousHash string
	Timestamp    int64
	Data         string
	Hash         string
}

type Message struct {
	Type MessageType `json: "type"`
	Data []*Block    `json: "data"`
}

const (
	QueryLatest MessageType = iota
	QueryAll
	ResponseBlockchain
)

var (
	httpPort     = flag.Int("httpPort", 3000, "http server port")
	initialPeers = flag.String("initialPeers", "", "initial peers")
)

var (
	genesisBlock = &Block{
		Index:        0,
		PreviousHash: "0",
		Timestamp:    1465154705,
		Data:         "my genesis block!!",
		Hash:         "816534932c2b7154836da6afc367695e6337db8a921823784c14378abed4f7d7",
	}
	blockchain = []*Block{genesisBlock}
)

var (
	upgrader = websocket.Upgrader{}
	sockets  = []*websocket.Conn{}
)

func calculateHash(index int, previousHash string, timestamp int64, data string) string {
	content := fmt.Sprintf("%d%v%d%v", index, previousHash, timestamp, data)
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

func calculateHashForBlock(block *Block) string {
	return calculateHash(block.Index, block.PreviousHash, block.Timestamp, block.Data)
}

func isValidNewBlock(newBlock, previousBlock *Block) bool {
	if previousBlock.Index+1 != newBlock.Index {
		log.Println("invalid index")
		return false
	}
	if previousBlock.Hash != newBlock.PreviousHash {
		log.Println("invalid previoushash")

		return false
	}
	if calculateHashForBlock(newBlock) != newBlock.Hash {
		log.Println("invalid hash")
		return false
	}
	return true
}

func getLastBlock() *Block {
	return blockchain[len(blockchain)-1]
}

func generateNextBlock(blockdata string) *Block {
	previousBlock := getLastBlock()
	nextIndex := previousBlock.Index + 1
	nextTimeStamp := time.Now().Unix()
	nextHash := calculateHash(nextIndex, previousBlock.Hash, nextTimeStamp, blockdata)
	return &Block{
		Index:        nextIndex,
		PreviousHash: previousBlock.Hash,
		Timestamp:    nextTimeStamp,
		Hash:         nextHash,
		Data:         blockdata,
	}
}

func addBlock(block *Block) {
	blockchain = append(blockchain, block)
}

func queryChainLengthMsg() *Message {
	return &Message{Type: QueryLatest}
}

func queryAllMsg() *Message {
	return &Message{Type: QueryAll}
}

func responseChainMsg() *Message {
	return &Message{Type: ResponseBlockchain, Data: blockchain}
}

func responseLatestMsg() *Message {
	return &Message{Type: ResponseBlockchain, Data: []*Block{getLastBlock()}}
}

func write(ws *websocket.Conn, message *Message) {
	err := ws.WriteJSON(message)
	if err != nil {
		log.Println("write:", err)
		closeConnection(ws)
		return
	}
}

func broadcast(message *Message) {
	for _, ws := range sockets {
		write(ws, message)
	}
}

func closeConnection(ws *websocket.Conn) {
	log.Printf("connection failed to peer: : %s", ws.RemoteAddr().String())
	for i, v := range sockets {
		if v == ws {
			sockets[i], sockets[len(sockets)-1] = sockets[len(sockets)-1], nil
			sockets = sockets[:len(sockets)-1]
			break
		}
	}

}

func isValidChain(blockchainToValidate []*Block) bool {
	genesisToValidate, err := json.Marshal((blockchainToValidate[0]))
	if err != nil {
		return false
	}
	gBlock, err := json.Marshal(genesisBlock)
	if err != nil {
		return false
	}
	if string(genesisToValidate) != string(gBlock) {
		return false
	}
	var tempBlocks = []*Block{blockchainToValidate[0]}
	for i := 1; i < len(blockchainToValidate); i++ {
		if isValidNewBlock(blockchainToValidate[i], tempBlocks[i-1]) {
			tempBlocks = append(tempBlocks, blockchainToValidate[i])
		} else {
			return false
		}
	}
	return true
}

func replaceChain(newBlocks []*Block) {
	if isValidChain(newBlocks) && len(newBlocks) > len(blockchain) {
		log.Println("Received blockchain is valid. Replacing current blockchain with received blockchain")
		blockchain = newBlocks
		broadcast(responseLatestMsg())
	} else {
		log.Println("Received blockchain invalid")
	}
}

func handleBlockchainResponse(message *Message) {
	receivedBlocks := message.Data
	if len(receivedBlocks) == 0 {
		return
	}
	latestBlockReceived := receivedBlocks[len(receivedBlocks)-1]
	latestBlockHeld := getLastBlock()
	if latestBlockReceived.Index > latestBlockHeld.Index {
		log.Printf("blockchain possibly behind. We got: %d Peer got: %d", latestBlockHeld.Index, latestBlockReceived.Index)
		switch {
		case latestBlockHeld.Hash == latestBlockReceived.PreviousHash:
			{
				log.Println("We can append the received block to our chain")
				blockchain = append(blockchain, latestBlockReceived)
				broadcast(responseLatestMsg())
			}
		case len(receivedBlocks) == 1:
			{
				log.Println("We have to query the chain from our peer")
				broadcast(queryAllMsg())
			}
		default:
			{
				log.Println("Received blockchain is longer than current blockchain")
				replaceChain(receivedBlocks)
			}
		}
	} else {
		log.Printf("received blockchain is not longer than current blockchain. Do nothing")
	}
}

func initMessageHandler(ws *websocket.Conn) {
	for {
		_, message, err := ws.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			closeConnection(ws)
			return
		}
		log.Printf("Received message: %s", string(message))
		m := &Message{}
		if json.Unmarshal(message, m) == nil {
			switch m.Type {
			case QueryLatest:
				write(ws, responseLatestMsg())
				break
			case QueryAll:
				write(ws, responseChainMsg())
				break
			case ResponseBlockchain:
				handleBlockchainResponse(m)
				break
			}
		}
	}
}

func initConnection(ws *websocket.Conn) {
	sockets = append(sockets, ws)
	initMessageHandler(ws)
}

func connectToPeers(addressList []string) error {
	for _, add := range addressList {
		u := url.URL{Scheme: "ws", Host: add, Path: "/ws"}
		log.Printf("connecting to %s", u.String())
		c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
		if err != nil {
			log.Fatal("dial:", err)
		} else {
			initConnection(c)
		}

	}
	return nil
}

func writeJson(w *http.ResponseWriter, data interface{}) error {
	jData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	(*w).Header().Set("Content-Type", "application/json")
	(*w).Write(jData)
	return nil
}

func initHttpServer() error {
	http.HandleFunc("/blocks", func(w http.ResponseWriter, r *http.Request) {
		if err := writeJson(&w, blockchain); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	http.HandleFunc("/mineBlock", func(w http.ResponseWriter, r *http.Request) {
		b, err := ioutil.ReadAll(r.Body)
		defer r.Body.Close()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var data struct {
			Data string `json: "data"`
		}
		if err = json.Unmarshal(b, &data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		newBlock := generateNextBlock(string(data.Data))
		addBlock(newBlock)
		broadcast(responseLatestMsg())
		log.Println("block added")

	})
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("upgrade:", err)
			return
		}
		defer c.Close()
		initConnection(c)
	})
	http.HandleFunc("/peers", func(w http.ResponseWriter, r *http.Request) {
		var peers []string
		for _, socket := range sockets {
			if socket != nil {
				peers = append(peers, fmt.Sprintf("local addr: %v; remote addr: %v", socket.LocalAddr().String(), socket.RemoteAddr().String()))
			}
		}
		if err := writeJson(&w, peers); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	http.HandleFunc("/addPeer", func(w http.ResponseWriter, r *http.Request) {
		b, err := ioutil.ReadAll(r.Body)
		defer r.Body.Close()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var peer struct {
			Address string `json: "address"`
		}
		if err = json.Unmarshal(b, &peer); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		go connectToPeers([]string{peer.Address})
	})
	return http.ListenAndServe(":"+strconv.Itoa(*httpPort), nil)
}

func main() {
	flag.Parse()
	log.SetFlags(0)
	slice := strings.Split(*initialPeers, ",")
	if len(slice) > 0 && slice[0] != "" {
		go connectToPeers(slice)
	}
	if err := initHttpServer(); err != nil {
		log.Fatal("init http server:", err)
		os.Exit(1)
	}
}
