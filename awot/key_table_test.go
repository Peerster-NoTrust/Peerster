// Tests for KeyTable
package awot

import (
  "testing"
  "crypto/rsa"
  "crypto/rand"
)

func TestAddRetrieve(t *testing.T) {

  table := NewKeyTable()

  _, present := table.Get("node1")
  if present {
    t.Errorf("retrieving unknown key returns")
  }

  r1K, err := rsa.GenerateKey(rand.Reader, 4096)
  if err != nil {
    t.Errorf("error generating key")
  }

  r1 := TrustedKeyRecord {
    record: KeyRecord {
      Owner: "node1",
      KeyPub: r1K.PublicKey,
    },
  }

  table.Add(r1)
  pk1, present := table.Get("node1")

  if !present {
    t.Errorf("cannot retrieve existing key")
  }

  if r1K.PublicKey != pk1 {
    t.Errorf("keys are different")
  }
}