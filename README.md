# Naivechain-Go

A simple go implemention of Naivechain (https://github.com/lhartikk/naivechain).

### Quick start
```
go build 
./naivechain-go
./naivechain-go -httpPort=4000 -
```

### HTTP API
##### Get blockchain
```
curl http://localhost:3000/blocks
```
##### Create block
```
curl -H "Content-type:application/json" --data '{"data" : "Some data to the first block"}' http://localhost:3000/mineBlock
``` 
##### Add peer
```
curl -H "Content-type:application/json" --data '{"peer" : "ws://localhost:4000"}' http://localhost:3000/addPeer
```
#### Query connected peers
```
curl http://localhost:3000/peers
```