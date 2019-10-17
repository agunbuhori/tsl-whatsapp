package main

import (
	"encoding/gob"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
	"context"
	"strings"
	"net/http"
	"regexp"
	"github.com/Rhymen/go-whatsapp/binary/proto"

	qrcodeTerminal "github.com/Baozisoftware/qrcode-terminal-go"
	"github.com/Rhymen/go-whatsapp"
	
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func GetClient() *mongo.Client {
    clientOptions := options.Client().ApplyURI("mongodb://localhost:27017")
	client, err := mongo.NewClient(clientOptions)
	
    if err != nil {
        log.Fatal(err)
	}
	
    err = client.Connect(context.Background())
    if err != nil {
        log.Fatal(err)
	}
	
    return client
}

var c = GetClient()

func enableCors(w *http.ResponseWriter) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
}

func main() {
	// readMessage()
	
	err := c.Ping(context.Background(), readpref.Primary())
    if err != nil {
        log.Fatal("Couldn't connect to the database", err)
    } else {
        log.Println("Connected!")
    }

	readMessage()
}


type Sender struct {
	Number string
	Message string
}

func createSender(client *mongo.Client, sender Sender) interface{} {
    collection := client.Database("tsl").Collection("senders")
    insertResult, err := collection.InsertOne(context.TODO(), sender)
    if err != nil {
        log.Fatalln("Error on inserting new Hero", err)
    }
    return insertResult
}

type waHandler struct {
	c *whatsapp.Conn
}

//HandleError needs to be implemented to be a valid WhatsApp handler
func (h *waHandler) HandleError(err error) {

	if e, ok := err.(*whatsapp.ErrConnectionFailed); ok {
		log.Printf("Connection failed, underlying error: %v", e.Err)
		log.Println("Waiting 30sec...")
		<-time.After(30 * time.Second)
		log.Println("Reconnecting...")
		err := h.c.Restore()
		if err != nil {
			log.Fatalf("Restore failed: %v", err)
		}
	} else {
		log.Printf("error occoured: %v\n", err)
	}
}

type Registrant struct {
	Name string
	Age int
	Gender string
	Country string
	Province string
	City string
	WhatsApp string
}

func updateRegistrant(client *mongo.Client, whatsapp string, docID string) {
	collection := client.Database("tsl").Collection("registrants")
	
	objID, err := primitive.ObjectIDFromHex(docID)
	filter := bson.M{"_id": bson.M{"$eq": objID}}
	update := bson.M{
		"$set": bson.M{
			"whatsapp": ""+whatsapp+"",
		},
	}

	updateResult, err := collection.UpdateOne(context.TODO(), filter, update)
    if err != nil {
        log.Fatalln("Error on inserting new Hero", err)
	}
	
	fmt.Println("UpdateOne() result:", updateResult)

	sendMessage(whatsapp, "Pendaftaran berhasil, berikut profil antum : http://52.221.245.243/profile/"+docID);
}

//Optional to be implemented. Implement HandleXXXMessage for the types you need.
func (*waHandler) HandleTextMessage(message whatsapp.TextMessage) {
	// fmt.Printf("%v %v %v %v\n\t%v\n", message.Info.Timestamp, message.Info.Id, message.Info.RemoteJid, message.Info.QuotedMessageID, message.Text)
	min := time.Now().Unix() - int64(message.Info.Timestamp)

	if (min < 60) {

		err := c.Ping(context.Background(), readpref.Primary())
		
		if err != nil {
			log.Fatal("Couldn't connect to the database", err)
		}

		replacer := strings.NewReplacer("@s.whatsapp.net", "")
		WANumber := replacer.Replace(message.Info.RemoteJid)
		regex, _ := regexp.Compile(`\[[a-z0-9]+\]`)
		str := regex.FindString(message.Text)
		trim1 := strings.Trim(str, "[")
		trim2 := strings.Trim(trim1, "]")
		sender := Sender{Number: WANumber, Message: trim2}
		
		log.Printf("%v => %v", message.Info.RemoteJid, trim2)
		createSender(c, sender)
		updateRegistrant(c, WANumber, trim2)
	}
}

func readMessage() {
	//create new WhatsApp connection
	wac, err := whatsapp.NewConn(5 * time.Second)
	if err != nil {
		log.Fatalf("error creating connection: %v\n", err)
	}

	//Add handler
	wac.AddHandler(&waHandler{wac})
	//login or restore
	if err := login(wac); err != nil {
		log.Fatalf("error logging in: %v\n", err)
	}
	
	//verifies phone connectivity
	pong, err := wac.AdminTest()
	
	if !pong || err != nil {
		log.Fatalf("error pinging in: %v\n", err)
	}
	
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	//Disconnect safe
	fmt.Println("Shutting down now.")
	session, err := wac.Disconnect()
	if err != nil {
		log.Fatalf("error disconnecting: %v\n", err)
	}
	if err := writeSession(session); err != nil {
		log.Fatalf("error saving session: %v", err)
	}
}

func sendMessage(number string, message string) {
	//create new WhatsApp connection
	wac, err := whatsapp.NewConn(5 * time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating connection: %v\n", err)
		return
	}

	err = login(wac)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error logging in: %v\n", err)
		return
	}

	<-time.After(3 * time.Second)

	previousMessage := "😘"
	quotedMessage := &proto.Message{
		Conversation: &previousMessage,
	}

	msg := whatsapp.TextMessage{
		Info: whatsapp.MessageInfo{
			RemoteJid:     number+"@s.whatsapp.net",
			QuotedMessage: *quotedMessage, //you also must send a valid QuotedMessageID
		},
		Text: message,
	}

	msgId, err := wac.Send(msg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error sending message: %v", err)
		os.Exit(1)
	} else {
		fmt.Println("Message Sent -> ID : " + msgId)
	}
}


func login(wac *whatsapp.Conn) error {
	//load saved session
	session, err := readSession()
	if err == nil {
		//restore session
		session, err = wac.RestoreWithSession(session)
		if err != nil {
			return fmt.Errorf("restoring failed: %v\n", err)
		}
	} else {
		//no saved session -> regular login
		qr := make(chan string)
		go func() {
			terminal := qrcodeTerminal.New()
			terminal.Get(<-qr).Print()
		}()
		session, err = wac.Login(qr)
		if err != nil {
			return fmt.Errorf("error during login: %v\n", err)
		}
	}

	//save session
	err = writeSession(session)
	if err != nil {
		return fmt.Errorf("error saving session: %v\n", err)
	}
	return nil
}

func readSession() (whatsapp.Session, error) {
	session := whatsapp.Session{}
	file, err := os.Open("../sessions//whatsappSession.gob")
	if err != nil {
		return session, err
	}
	defer file.Close()
	decoder := gob.NewDecoder(file)
	err = decoder.Decode(&session)
	if err != nil {
		return session, err
	}
	return session, nil
}

func writeSession(session whatsapp.Session) error {
	file, err := os.Create("../sessions//whatsappSession.gob")
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := gob.NewEncoder(file)
	err = encoder.Encode(session)
	if err != nil {
		return err
	}
	return nil
}
