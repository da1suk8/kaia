package main

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
)

func main() {
	// 1. Load resources
	key := loadKey("key.pem")
	msg, err := os.ReadFile("msg.bin")
	if err != nil {
		// Treat missing msg.bin as empty message for edge-case generation.
		msg = []byte{}
	}

	// 2. Hash & Sign
	h := sha256.Sum256(msg)
	hash := h[:]
	r, s, err := ecdsa.Sign(rand.Reader, key, hash)
	if err != nil {
		panic(err)
	}

	// 3. Low-S Normalize (secp256r1)
	n, _ := new(big.Int).SetString("ffffffff00000000ffffffffffffffffbce6faada7179e84f3b9cac2fc632551", 16)
	if s.Cmp(new(big.Int).Rsh(n, 1)) > 0 {
		s.Sub(n, s)
	}

	if os.Getenv("INVALID_SCALAR_ZERO") == "true" {
		r = new(big.Int)
	}

	qx, qy := key.X, key.Y
	if os.Getenv("INVALID_KEY_ZERO") == "true" {
		qx, qy = new(big.Int), new(big.Int)
	} else if os.Getenv("INVALID_KEY_CURVE") == "true" {
		// (1, 1) is not on secp256r1
		qx, qy = new(big.Int).SetInt64(1), new(big.Int).SetInt64(1)
	}

	// 4. Output: hash|r|s|x|y
	var out []byte
	out = append(out, hash...)
	out = append(out, to32(r)...)
	out = append(out, to32(s)...)
	out = append(out, to32(qx)...)
	out = append(out, to32(qy)...)

	fmt.Printf("input hex: 0x%s\n", hex.EncodeToString(out))
}

func loadKey(filename string) *ecdsa.PrivateKey {
	data, _ := os.ReadFile(filename)
	block, _ := pem.Decode(data)
	// Try EC key first, then PKCS8
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		panic(err)
	}
	return key.(*ecdsa.PrivateKey)
}

func to32(i *big.Int) []byte {
	b := make([]byte, 32)
	i.FillBytes(b)
	return b
}
