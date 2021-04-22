package blockchain

import (
	"encoding/hex"
	"fmt"
	"github.com/dgraph-io/badger"
	"os"
	"runtime"
)

const (
	databasePath = "./tmp/blocks"
	databaseFile = "./tmp/blocks/MANIFEST"
	genesisData  = "First Transaction from Genesis"
)

type BlockChain struct {
	LastHash []byte
	Database *badger.DB
}

type Iterator struct {
	CurrentHash []byte
	Database    *badger.DB
}

func DatabaseExists() bool {
	if _, err := os.Stat(databaseFile); os.IsNotExist(err) {
		return false
	}

	return true
}

func NewBlockChain(address string) *BlockChain {
	var lastHash []byte

	if DatabaseExists() {
		fmt.Println("Blockchain already exists")
		runtime.Goexit()
	}

	options := badger.DefaultOptions(databasePath)
	options.Dir = databasePath
	options.ValueDir = databasePath
	options.Logger = nil

	db, err := badger.Open(options)
	Handle(err)

	err = db.Update(func(txn *badger.Txn) error {
		coinbaseTx := CoinbaseTx(address, genesisData)
		genesis := Genesis(coinbaseTx)
		fmt.Println("Genesis created")

		err = txn.Set(genesis.Hash, genesis.Serialize())
		Handle(err)

		err = txn.Set([]byte("lh"), genesis.Hash)
		lastHash = genesis.Hash

		return err
	})

	Handle(err)

	blockchain := BlockChain{lastHash, db}
	return &blockchain
}

func ContinueBlockChain(address string) *BlockChain {
	if DatabaseExists() == false {
		fmt.Println("No existing blockchain found, create one!")
		runtime.Goexit()
	}

	var lastHash []byte

	options := badger.DefaultOptions(databasePath)
	options.Dir = databasePath
	options.ValueDir = databasePath
	options.Logger = nil

	db, err := badger.Open(options)
	Handle(err)

	err = db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("lh"))
		Handle(err)

		err = item.Value(func(val []byte) error {
			lastHash = val
			return nil
		})

		return err
	})

	Handle(err)

	chain := BlockChain{lastHash, db}

	return &chain
}

func (c *BlockChain) AddBlock(transactions []*Transaction) {
	var lastHash []byte

	err := c.Database.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("lh"))
		Handle(err)

		err = item.Value(func(val []byte) error {
			lastHash = val
			return nil
		})

		return err
	})

	Handle(err)

	newBlock := CreateBlock(transactions, lastHash)

	err = c.Database.Update(func(txn *badger.Txn) error {
		err := txn.Set(newBlock.Hash, newBlock.Serialize())
		Handle(err)

		err = txn.Set([]byte("lh"), newBlock.Hash)
		c.LastHash = newBlock.Hash

		return err
	})

	Handle(err)
}

func (c *BlockChain) Iterator() *Iterator {
	iter := &Iterator{c.LastHash, c.Database}

	return iter
}

func (i *Iterator) Next() *Block {
	var block *Block

	err := i.Database.View(func(txn *badger.Txn) error {
		item, err := txn.Get(i.CurrentHash)
		Handle(err)

		err = item.Value(func(val []byte) error {
			block = Deserialize(val)
			return nil
		})

		return err
	})

	Handle(err)

	i.CurrentHash = block.PrevHash

	return block
}

func (c *BlockChain) FindUnspentTransactions(address string) []Transaction {
	var unspentTransactions []Transaction

	spentTransactionOutputs := make(map[string][]int)

	iter := c.Iterator()

	for {
		block := iter.Next()

		for _, tx := range block.Transactions {
			txId := hex.EncodeToString(tx.ID)

		Outputs:
			for outIndex, out := range tx.Outputs {
				if spentTransactionOutputs[txId] != nil {
					for _, spentOut := range spentTransactionOutputs[txId] {
						if spentOut == outIndex {
							continue Outputs
						}
					}
				}
				if out.CanBeUnlocked(address) {
					unspentTransactions = append(unspentTransactions, *tx)
				}
			}

			if tx.IsCoinbase() == false {
				for _, in := range tx.Inputs {
					if in.CanUnlock(address) {
						inTxID := hex.EncodeToString(in.ID)
						spentTransactionOutputs[inTxID] = append(spentTransactionOutputs[inTxID], in.Out)
					}
				}
			}
		}

		if len(block.PrevHash) == 0 {
			break
		}
	}

	return unspentTransactions
}

func (c *BlockChain) FindUnspentTransactionsOutput(address string) []TxOutput {
	var unspentTransactionOutputs []TxOutput
	unspentTransactions := c.FindUnspentTransactions(address)

	for _, tx := range unspentTransactions {
		for _, out := range tx.Outputs {
			if out.CanBeUnlocked(address) {
				unspentTransactionOutputs = append(unspentTransactionOutputs, out)
			}
		}
	}

	return unspentTransactionOutputs
}

func (c *BlockChain) FindSpendableOutput(address string, amount int) (int, map[string][]int) {
	unspentOuts := make(map[string][]int)
	unspentTxs := c.FindUnspentTransactions(address)
	accumulated := 0

Work:
	for _, tx := range unspentTxs {
		txId := hex.EncodeToString(tx.ID)

		for outIndex, out := range tx.Outputs {
			if out.CanBeUnlocked(address) && accumulated < amount {
				accumulated += out.Value
				unspentOuts[txId] = append(unspentOuts[txId], outIndex)

				if accumulated >= amount {
					break Work
				}
			}
		}
	}

	return accumulated, unspentOuts
}
