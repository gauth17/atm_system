package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

type Account struct {
	AccountNumber string  `bson:"account_number" json:"account_number"`
	Name          string  `bson:"name" json:"name"`
	Pin           string  `bson:"pin" json:"-"`
	Balance       float64 `bson:"balance" json:"balance"`
}

type Transaction struct {
	From     string  `bson:"from" json:"from"`
	To       string  `bson:"to" json:"to"`
	Type     string  `bson:"type" json:"type"`
	Amount   float64 `bson:"amount" json:"amount"`
	DateTime string  `bson:"datetime" json:"datetime"`
}

type CreateAccountRequest struct {
	Name string `json:"name" binding:"required"`
	Pin  string `json:"pin" binding:"required"`
}

type DepositRequest struct {
	AccountNumber string  `json:"account_number" binding:required`
	Pin           string  `json:"pin" binding:"required"`
	Amount        float64 `json:"amount" binding:"required"`
}

type WithdrawRequest struct {
	AccountNumber string  `json:"account_number" binding:"required"`
	Pin           string  `json:"pin" binding:"required"`
	Amount        float64 `json:"amount" binding:"required"`
}

type TransferRequest struct {
	FromAccount string  `json:"from_account" binding:"required"`
	FromPin     string  `json:"from_pin" binding:"required"`
	ToAccount   string  `json:"to_account" binding:"required"`
	Amount      float64 `json:"amount" binding:"required"`
}

type PinRequest struct {
	AccountNumber string `json:"account_number" binding:"required"`
	OldPin        string `json:"old_pin" binding:"required"`
	NewPin        string `json:"new_pin" binding:"required"`
}

func hashPassword(pin string) string {
	hash := sha256.Sum256([]byte(pin))
	return hex.EncodeToString(hash[:])
}

func UserRoute(r *gin.Engine) {
	r.POST("/create", CreateAccount)
	r.POST("/deposit", Deposit)
	r.POST("/withdraw", Withdraw)
	r.POST("/transfer", Transfer)
	r.POST("/setpin", SetPin)
	r.POST("/bankstatement", BankStatement)
}

var (
	client *mongo.Client
	err    error
)

func init() {

	client, err = mongo.Connect(context.Background(), options.Client().ApplyURI("mongodb+srv://Gautham:dbpassword@cluster0.coh9m7c.mongodb.net/?retryWrites=true&w=majority"))
	if err != nil {
		log.Fatal(err)
	}

	err = client.Ping(context.Background(), readpref.Primary())
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	r := gin.Default()

	UserRoute(r)

	r.Run("localhost:9003")
}

func CreateAccount(c *gin.Context) {
	var req CreateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	accountNumber := fmt.Sprintf("%06d", rand.Intn(1000000))

	// filter := bson.M{"account_number": accountNumber}
	// var existingAccount Account
	// err := client.Database("atm").Collection("accounts").FindOne(context.Background(), filter).Decode(&existingAccount)
	// if err == nil {
	// 	c.JSON(http.StatusBadRequest, gin.H{"error": "account already exists"})
	// 	return
	// }

	if len(req.Pin) != 4 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pin must be 4 digits"})
		return
	}

	newAccount := Account{
		Name:          req.Name,
		AccountNumber: accountNumber,
		Pin:           hashPassword(req.Pin),
		Balance:       0,
	}
	_, err = client.Database("atm").Collection("accounts").InsertOne(context.Background(), newAccount)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create account"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"User account created successfully. Account number": accountNumber})
}

func Deposit(c *gin.Context) {
	var req DepositRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	filter := bson.M{"account_number": req.AccountNumber, "pin": hashPassword(req.Pin)}
	var existingAccount Account
	err := client.Database("atm").Collection("accounts").FindOne(context.Background(), filter).Decode(&existingAccount)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account number or PIN"})
		return
	}

	existingAccount.Balance += req.Amount
	update := bson.M{"$set": bson.M{"balance": existingAccount.Balance}}
	_, err = client.Database("atm").Collection("accounts").UpdateOne(context.Background(), filter, update)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to deposit money"})
		return
	}

	transaction := Transaction{
		From:     "",
		To:       req.AccountNumber,
		Type:     "deposit",
		Amount:   req.Amount,
		DateTime: time.Now().Format(time.RFC3339),
	}
	_, err = client.Database("atm").Collection("transactions").InsertOne(context.Background(), transaction)
	if err != nil {
		log.Println("failed to insert transaction record:", err)
	}

	c.JSON(http.StatusOK, gin.H{"message": "amount deposited successfully"})
}

func Withdraw(c *gin.Context) {
	var req WithdrawRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	filter := bson.M{"account_number": req.AccountNumber, "pin": hashPassword(req.Pin)}
	var existingAccount Account
	err := client.Database("atm").Collection("accounts").FindOne(context.Background(), filter).Decode(&existingAccount)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account number or PIN"})
		return
	}

	if existingAccount.Balance < req.Amount {
		c.JSON(http.StatusBadRequest, gin.H{"error": "not enough balance in account"})
		return
	}

	existingAccount.Balance -= req.Amount
	update := bson.M{"$set": bson.M{"balance": existingAccount.Balance}}
	_, err = client.Database("atm").Collection("accounts").UpdateOne(context.Background(), filter, update)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to withdraw amount"})
		return
	}

	transaction := Transaction{
		From:     "",
		To:       req.AccountNumber,
		Type:     "withdraw",
		Amount:   req.Amount,
		DateTime: time.Now().Format(time.RFC3339),
	}
	_, err = client.Database("atm").Collection("transactions").InsertOne(context.Background(), transaction)
	if err != nil {
		log.Println("failed to insert transaction record:", err)
	}

	c.JSON(http.StatusOK, gin.H{"message": "amount withdrawn successfully"})

}

func Transfer(c *gin.Context) {
	var req TransferRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	filterFrom := bson.M{"account_number": req.FromAccount, "pin": hashPassword(req.FromPin)}
	var fromAccount Account
	err := client.Database("atm").Collection("accounts").FindOne(context.Background(), filterFrom).Decode(&fromAccount)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'from' account number or PIN"})
		return
	}

	filterTo := bson.M{"account_number": req.ToAccount}
	var toAccount Account
	err = client.Database("atm").Collection("accounts").FindOne(context.Background(), filterTo).Decode(&toAccount)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'to' account number"})
		return
	}

	if fromAccount.Balance < req.Amount {
		c.JSON(http.StatusBadRequest, gin.H{"error": "not enough balance in 'from' account"})
		return
	}

	fromAccount.Balance -= req.Amount
	updateFrom := bson.M{"$set": bson.M{"balance": fromAccount.Balance}}
	_, err = client.Database("atm").Collection("accounts").UpdateOne(context.Background(), filterFrom, updateFrom)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to transfer money"})
		return
	}

	toAccount.Balance += req.Amount
	updateTo := bson.M{"$set": bson.M{"balance": toAccount.Balance}}
	_, err = client.Database("atm").Collection("accounts").UpdateOne(context.Background(), filterTo, updateTo)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to transfer money"})
		return
	}

	transactionFrom := Transaction{
		From:     req.FromAccount,
		To:       req.ToAccount,
		Type:     "withdraw",
		Amount:   req.Amount,
		DateTime: time.Now().Format(time.RFC3339),
	}
	_, err = client.Database("atm").Collection("transactions").InsertOne(context.Background(), transactionFrom)
	if err != nil {
		log.Println("failed to insert transaction record:", err)
	}

	transactionTo := Transaction{
		From:     req.FromAccount,
		To:       req.ToAccount,
		Type:     "deposit",
		Amount:   req.Amount,
		DateTime: time.Now().Format(time.RFC3339),
	}
	_, err = client.Database("atm").Collection("transactions").InsertOne(context.Background(), transactionTo)
	if err != nil {
		log.Println("failed to insert transaction record:", err)
	}

	c.JSON(http.StatusOK, gin.H{"message": "money transferred successfully"})
}

func SetPin(c *gin.Context) {
	var req PinRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	filter := bson.M{"account_number": req.AccountNumber, "pin": hashPassword(req.OldPin)}
	var account Account
	err := client.Database("atm").Collection("accounts").FindOne(context.Background(), filter).Decode(&account)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account number or PIN"})
		return
	}

	update := bson.M{"$set": bson.M{"pin": hashPassword(req.NewPin)}}
	_, err = client.Database("atm").Collection("accounts").UpdateOne(context.Background(), filter, update)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update PIN"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "PIN updated successfully"})
}

func BankStatement(c *gin.Context) {
	var req struct {
		AccountNumber string `json:"account_number"`
		Pin           string `json:"pin"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	filter := bson.M{"account_number": req.AccountNumber}
	var account Account
	err := client.Database("atm").Collection("accounts").FindOne(context.Background(), filter).Decode(&account)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account number"})
		return
	}

	if account.Pin != hashPassword(req.Pin) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid PIN"})
		return
	}

	filter = bson.M{"$or": []interface{}{
		bson.M{"from": req.AccountNumber},
		bson.M{"to": req.AccountNumber},
	}}
	cursor, err := client.Database("atm").Collection("transactions").Find(context.Background(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve transaction history"})
		return
	}
	defer cursor.Close(context.Background())

	var transactions []Transaction
	for cursor.Next(context.Background()) {
		var transaction Transaction
		if err := cursor.Decode(&transaction); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decode transaction"})
			return
		}
		transactions = append(transactions, transaction)
	}

	c.JSON(http.StatusOK, gin.H{"transactions": transactions})
}
