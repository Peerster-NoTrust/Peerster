package awot

import (
	"container/list"
	"crypto/rsa"
	"errors"
	"log"
	"math"
	"sync"
	"time"

	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/path"
	"gonum.org/v1/gonum/graph/simple"
)

// A Node is a node in the key ring, representing a peer in the network
type Node struct {
	name        string
	id          int64
	probability *float32
}

// An Edge is a directed edge F->T in the key ring, representing that F signed the key for T
type Edge struct {
	F, T Node
	Key  rsa.PublicKey
}

// From returns the from-node of the edge.
func (e Edge) From() graph.Node { return e.F }

// To returns the to-node of the edge.
func (e Edge) To() graph.Node { return e.T }

// ID returns the integer ID of a node
func (n Node) ID() int64 {
	return int64(n.id)
}

// A ReputationTable is the interface that wraps the Reputation function
// Reputation returns a reputation of a node with given name, as a float32 between 0 and 1,
// 0 being the worst reputation and 1 the best. It also returns a boolean informing if the reputation actually exists.
type ReputationTable interface {
	Reputation(string) (float32, bool)
}

// A KeyRing is a directed graph of Node and Edge
type KeyRing struct {
	source       string               // the id of the source in the keyring
	ids          map[string]*Node     // name -> Node mapping
	graph        simple.DirectedGraph // graph
	nextNode     int64                // for instanciating new nodes
	keyTable                          // for updates
	pending      *list.List           // pending KeyExchangeMessage
	pendingMutex *sync.Mutex          // mutex for pending KeyExchangeMessage
	mutex        *sync.Mutex          // mutex for the keyring itself
	threshold    float32              // confidence threshold for trusted keys
	stopped      bool                 // indicator for the state of the ring
}

////////// Key Ring API

// NewKeyRing creates a new key-ring given some fully trusted (origin-public key) pairs.
// For updating the KeyRing, use KeyRing.Start() after creation.
// Parameters :
// 	owner : the name (id) of the owner of the keychain (typically this network node)
// 	key : the public key of owner
// 	trustedRecords : the fully trusted bootstrap records : trusted public keys of initiators
// 	threshold : the confidence threshold; below it the keys will not be given to the user
func NewKeyRing(owner string, key rsa.PublicKey, trustedRecords []TrustedKeyRecord, threshold float32) KeyRing {

	keyTable := newKeyTable(owner, key)
	nextNode := int64(0)

	// map
	ids := make(map[string]*Node)

	// create empty graph
	graph := simple.NewDirectedGraph()

	p := float32(1.0)
	// add source to graph
	source := Node{
		name:        owner,
		id:          nextNode,
		probability: &p,
	}
	nextNode += 1
	graph.AddNode(source)
	// set id and name association in map
	ids[owner] = &source
	// add key
	keyTable.add(TrustedKeyRecord{
		KeyRecord: KeyRecord{
			Owner:  owner,
			KeyPub: key,
		},
		Confidence: 1.0,
	})

	// add each fully trusted key
	for _, rec := range trustedRecords {
		// add node to graph
		p := float32(1.0)
		node := Node{
			name:        rec.KeyRecord.Owner,
			id:          nextNode,
			probability: &p,
		}
		nextNode += 1
		graph.AddNode(node)
		// set id and name association in map
		ids[rec.Owner] = &node

		// add edge from source to new node

		// add edge from source
		edge := Edge{
			F:   source,
			T:   node,
			Key: rec.KeyPub,
		}
		graph.SetEdge(edge)

		// add key
		keyTable.add(TrustedKeyRecord{
			KeyRecord: KeyRecord{
				Owner:  rec.Owner,
				KeyPub: rec.KeyPub,
			},
			Confidence: rec.Confidence,
		})

	}

	ring := KeyRing{
		source:       owner,
		ids:          ids,
		graph:        *graph,
		nextNode:     nextNode,
		keyTable:     keyTable,
		pending:      list.New(),
		pendingMutex: &sync.Mutex{},
		mutex:        &sync.Mutex{},
		threshold:    threshold,
		stopped:      false,
	}
	// return
	return ring
}

// Start starts the updates on the KeyRing
// It spawns a goroutine that will update the keyring regularly at the given rate
func (ring *KeyRing) Start(rate time.Duration) {
	go ring.worker(rate, nil)
}

// StartWithReputation starts the updates on the KeyRing using the given ReputationTable for some of them
// It spawns a goroutine that will update the keyring regularly, at given rate
func (ring *KeyRing) StartWithReputation(rate time.Duration, reptable ReputationTable) {
	go ring.worker(rate, reptable)
}

// Stop stops the KeyRing.
// It will keep the state of the ring, but any later add will not update the confidence levels.
func (ring *KeyRing) Stop() {
	ring.stopped = true

	// TODO wait for the thread to stop
}

// GetKey returns the key of peer with given name and true if it exists, otherwise returns false.
// If the confidence level is too low for the key, it does not return the key and reports as if there where none.
// This should be used e.g. when trying to communicate with a peer and threfore needing its key.
func (ring KeyRing) GetKey(name string) (rsa.PublicKey, bool) {
	rec, ok := ring.keyTable.get(name)
	if !ok {
		return rec.KeyPub, ok
	}

	if rec.Confidence < ring.threshold {
		return rsa.PublicKey{}, false
	}

	return rec.KeyPub, ok
}

// GetRecord returns the record of peer with given name and true if it exists, otherwise returns false.
// Returns the record even if the confidence level is lower than the threshold.
// This should be used e.g. when updating reputation of a peer.
func (ring KeyRing) GetRecord(name string) (TrustedKeyRecord, bool) {
	return ring.keyTable.get(name)
}

// GetPeerList returns the list of peer names the keyring has a public key for
func (ring KeyRing) GetPeerList() []string {
	return ring.keyTable.getPeerList()
}

// AddUnverified adds a KeyExchangeMessage that could not yet be verified (e.g. lack of signer's key)
func (ring *KeyRing) AddUnverified(msg KeyExchangeMessage) {
	ring.mutex.Lock()
	defer ring.mutex.Unlock()
	ring.pending.PushBack(msg)
}

// Add updates the key ring with the given (verified) keyrecord and origin of the signature
// It assumes that the record's signature has been verified
func (ring *KeyRing) Add(rec KeyRecord, sigOrigin string, reputationOwner float32) {
	// do not update if the signer is unknown
	if !ring.contains(sigOrigin) {
		return
	}

	if rec.Owner == sigOrigin {
		// self signed record
		return
	}

	// add owner of the key if not yet known, or update its probability
	ring.addNode(rec.Owner, 0.0)

	// add edge
	err := ring.addEdge(sigOrigin, rec.Owner, rec.KeyPub)

	if err != nil {
		log.Fatal("KeyRing Add : could not add edge")
	}

	// recompute its probability
	probability := ring.phi(rec.Owner, reputationOwner)

	// update probability
	ring.addNode(rec.Owner, probability)

	// save key with "unknown" confidence, that will be computed after
	// ring.keyTable.add(TrustedKeyRecord{
	// 	Record:     rec,
	// 	Confidence: float32(0.0),
	// })

	// // recompute the confidence of the keys
	ring.updateConfidence()
}

////////// Key Ring Implementation

// worker performs periodic updates on a keyring, at given rate
func (ring *KeyRing) worker(rate time.Duration, reptable ReputationTable) {
	// updating the ring with yet unverified pending messages
	go func() {
		ticker := time.NewTicker(rate) // every 5 sec
		defer ticker.Stop()
		for range ticker.C {
			if ring.stopped {
				break
			}
			ring.updateTrust(reptable)
			ring.updatePending(reptable)
			ring.updateConfidence()
		}
	}()
}

// updateTrust recomputes the trust associated with each node to account for reputation updates or ring updates
func (ring *KeyRing) updateTrust(reptable ReputationTable) {

	for name := range ring.ids {
		present := false
		rep := float32(0.5)
		if reptable != nil {
			rep, present = reptable.Reputation(name)
		}
		if !present {
			rep = 0.5
		}
		probability := ring.phi(name, 2*rep)
		ring.addNode(name, probability)
	}
}

// updateConfidence updates the key table by computing new confidence levels for each key
func (ring *KeyRing) updateConfidence() {
	ring.mutex.Lock()
	defer ring.mutex.Unlock()

	allShortest := path.DijkstraAllPaths(&ring.graph)

	source := ring.graph.Node(ring.ids[ring.source].id)

	// compute for each node
	for terminalName, terminalVertex := range ring.ids {
		terminal := ring.graph.Node(terminalVertex.id)
		// get shortest paths from source to node
		minpaths, _ := allShortest.AllBetween(source, terminal)
		minpaths, bestKey := ring.selectBestPaths(minpaths)
		probability := probabilityOfMinPaths(minpaths)
		// update the key table
		ring.keyTable.updateConfidence(terminalName, probability, bestKey)
	}
}

// selectBestPaths takes a set of paths and returns the biggest subset in which all paths corresponds to the same end public key
// the key chosen is the one corresponding to the maximum number of paths
// thread unsafe
func (ring KeyRing) selectBestPaths(paths [][]graph.Node) ([][]graph.Node, *rsa.PublicKey) {
	if len(paths) == 0 {
		return paths, nil
	} else if len(paths) == 1 {
		p := paths[0]
		if len(p) < 2 {
			return paths, nil
		}
		s := p[len(p)-2]
		t := p[len(p)-1]

		edge := ring.graph.Edge(s, t)
		if edge == nil {
			log.Fatal("edge disappeared")
		}
		key := edge.(Edge).Key
		return paths, &key
	}

	occurrences := make(map[string]int)

	// the paths ends with the terminal
	// use the last edge if exists

	for _, p := range paths {
		if len(p) < 2 {
			// siging itself should not happen
			continue
		}
		s := p[len(p)-2]
		t := p[len(p)-1]

		edge := ring.graph.Edge(s, t)
		if edge == nil {
			log.Fatal("edge disappeared")
		}
		key := edge.(Edge).Key
		occurrences[key.N.String()+"-"+string(key.E)] += 1
	}

	// find max
	max := 0
	var bkey string
	for key, occ := range occurrences {
		if occ > max {
			max = occ
			bkey = key
		}
	}

	bestPaths := make([][]graph.Node, 0)
	var bestKey rsa.PublicKey
	for _, p := range paths {
		if len(p) < 2 {
			continue
		}
		s := p[len(p)-2]
		t := p[len(p)-1]

		edge := ring.graph.Edge(s, t)
		if edge == nil {
			log.Fatal("edge disappeared")
		}
		key := edge.(Edge).Key
		if bkey == key.N.String()+"-"+string(key.E) {
			bestPaths = append(bestPaths, p)
			bestKey = key
		}
	}

	return bestPaths, &bestKey
}

// updateMessage updates a KeyRing with given message, if update successful return true
// an update is successful if the update was performed or enough information is known to declare the message not correct / trustworthy
func (ring *KeyRing) updateMessage(msg KeyExchangeMessage, confidenceOwner float32) bool {
	receivedKey, err := DeserializeKey(msg.KeyBytes)
	if err != nil {
		return true
	}

	record := KeyRecord{
		Owner:  msg.Owner,
		KeyPub: receivedKey,
	}

	kpub, present := ring.GetKey(msg.Origin)

	if !present {
		// still do not have a public key
		return false
	}
	err = Verify(msg, kpub)

	if err == nil {
		ring.Add(record, msg.Origin, confidenceOwner)
		return true
	}
	return false
}

// updatePending tries to update the KeyRing with old pending messages
func (ring *KeyRing) updatePending(reptable ReputationTable) {
	ring.pendingMutex.Lock()
	defer ring.pendingMutex.Unlock()

	toRemove := list.New()
	for e := ring.pending.Front(); e != nil; e = e.Next() {

		msg := e.Value.(KeyExchangeMessage)
		ok := false
		reputationOwner := float32(0.5)
		if reptable != nil {
			reputationOwner, ok = reptable.Reputation(msg.Owner)
		}
		if !ok {
			reputationOwner = 0.5
		}
		if ring.updateMessage(msg, 2*reputationOwner) {
			toRemove.PushBack(e)
		}
	}

	for e := ring.pending.Front(); e != nil; e = e.Next() {
		ring.pending.Remove(e)
	}
}

// phi computes the probability of the node, independently of its current probability
// the probability is the trust put in a node for advertising public keys
func (ring KeyRing) phi(name string, reputation float32) float32 {
	ring.mutex.Lock()
	defer ring.mutex.Unlock()

	if ring.ids[name] == nil {
		// probability of non existing node is 0
		return 0.0
	}

	// phi = min(1/d, rep)

	destNode := ring.graph.Node(ring.ids[name].id)
	sourceNode := ring.graph.Node(ring.ids[ring.source].id)

	// compute the distance from source to destination
	shortest := path.DijkstraFrom(sourceNode, &ring.graph)
	distance := shortest.WeightTo(destNode)

	if distance == 0 {
		distance = 1
	}
	phi := math.Min(1.0/distance, float64(reputation))

	return float32(phi)
}

// contains checks if a node with given name exists in the key ring
func (ring KeyRing) contains(name string) bool {
	ring.mutex.Lock()
	defer ring.mutex.Unlock()
	_, present := ring.ids[name]
	return present
}

// lastNode returns the id of the node with highest id
func (ring KeyRing) lastNode() int64 {
	maxId := int64(0)
	nodes := ring.graph.Nodes()
	for n := range nodes {
		if int64(n) > maxId {
			maxId = int64(n)
		}
	}
	return maxId
}

// addNode adds a Vertex to the KeyRing with given name and probability, or updates the node with given probability if it exists
func (ring *KeyRing) addNode(name string, probability float32) {
	ring.mutex.Lock()
	defer ring.mutex.Unlock()

	// check if already in KeyRing
	if vp, present := ring.ids[name]; present {
		// update the probability
		*(vp.probability) = probability
		return
	}

	// add to graph
	node := Node{
		id:          ring.lastNode() + 1,
		name:        name,
		probability: &probability,
	}
	ring.nextNode += 1
	ring.graph.AddNode(node)
	ring.ids[name] = &node

	return
}

// addEdge adds a directed edge from node named a to node named b, given the public key associated with the signature from a of b's key (that is the supposed key of a)
func (ring *KeyRing) addEdge(a, b string, key rsa.PublicKey) error {
	ring.mutex.Lock()
	defer ring.mutex.Unlock()

	if _, aPresent := ring.ids[a]; !aPresent {
		// a is not present
		return errors.New("adding edge with source non present")
	}

	if _, bPresent := ring.ids[b]; !bPresent {
		// b is not present
		return errors.New("adding edge with destination non present")
	}

	if a == b {
		return errors.New("adding edge between same vertices")
	}

	vA := ring.ids[a]
	vB := ring.ids[b]

	ring.graph.SetEdge(Edge{F: *vA, T: *vB, Key: key})
	return nil
}
