package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	"monzoutils"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kms"
	uuid "github.com/satori/go.uuid"
	monzo "github.com/tjvr/go-monzo"
)

type userConfig struct {
	accessToken           string
	dryRun                bool
	forceTrigger          bool
	forceTriggerAmount    int64
	forceTriggerAccountID string
}

func notifyUser(cl monzo.Client, deposits []*monzo.DepositRequest, dryRun bool) error {
	numPots := len(deposits)
	totalDesposited := int64(0)
	for _, deposit := range deposits {
		totalDesposited += deposit.Amount
	}
	fmt.Printf("Total deposits: %s\n", monzoutils.FormatPenceToGbp(totalDesposited))

	title := "🛒 Payday Sweep"
	if dryRun {
		title += " (DRY RUN)"
	}

	return cl.CreateFeedItem(&monzo.FeedItem{
		AccountID: deposits[0].AccountID,
		Type:      "basic",
		URL:       "http://www.github.com/ojcm",
		Title:     title,
		Body: fmt.Sprintf("Transferred %s to %d pots",
			monzoutils.FormatPenceToGbp(totalDesposited), numPots),
		ImageURL: "https://raw.githubusercontent.com/golang-samples/gopher-vector/master/gopher.png",
	})
}

func shouldTriggerSweepFromTransaction(transaction *monzo.Transaction) bool {
	return transaction.Description == "MONTHLY SALARY"
}

func calcMoneyToSweep(cl monzo.Client, transaction *monzo.Transaction) (int64, error) {

	balance, err := cl.Balance(transaction.AccountID)
	if err != nil {
		return 0, err
	}

	return balance.Balance - transaction.Amount, nil
}

func getIdempotencyKey() string {
	return uuid.Must(uuid.NewV4()).String()
}

func calcDeposits(sweepAmount int64, sourceAccountID string, pots []*monzo.Pot) []*monzo.DepositRequest {
	// Chosen algorithm is to split equally.
	perPotAmount := sweepAmount / int64(len(pots))

	var deposits []*monzo.DepositRequest
	for _, pot := range pots {
		fmt.Printf("Creating deposit of %s to pot %s\n", monzoutils.FormatPenceToGbp(perPotAmount), pot.Name)
		deposits = append(deposits, &monzo.DepositRequest{
			PotID:          pot.ID,
			AccountID:      sourceAccountID,
			Amount:         perPotAmount,
			IdempotencyKey: getIdempotencyKey(),
		})
	}

	return deposits
}

func processTransaction(config userConfig, transaction *monzo.Transaction) error {

	var accountID string
	if config.forceTrigger {
		accountID = config.forceTriggerAccountID
	} else {
		accountID = transaction.AccountID
	}

	// Should we start the sweep
	if !shouldTriggerSweepFromTransaction(transaction) && !config.forceTrigger {
		return nil
	}

	// Get client for processing
	cl := monzoutils.GetClient(config.accessToken)

	// Calculate how much money to move
	var err error
	var sweepAmount int64
	if config.forceTrigger {
		sweepAmount, err = config.forceTriggerAmount, nil
	} else {
		sweepAmount, err = calcMoneyToSweep(cl, transaction)
	}
	if err != nil {
		return err
	}

	// Get the pots to move it to
	pots, err := monzoutils.GetActivePots(cl)
	if err != nil {
		return err
	}

	// Construct the deposits
	deposits := calcDeposits(sweepAmount, accountID, pots)

	//Send DepositRequests
	if !config.dryRun {
		err = monzoutils.ProcessDeposits(cl, deposits)
		if err != nil {
			return err
		}
	}

	notifyUser(cl, deposits, config.dryRun)

	return nil
}

func getAccessToken() string {
	encrypted := os.Getenv("accessToken")

	kmsClient := kms.New(session.New())
	decodedBytes, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		panic(err)
	}
	input := &kms.DecryptInput{
		CiphertextBlob: decodedBytes,
	}
	response, err := kmsClient.Decrypt(input)
	if err != nil {
		panic(err)
	}
	// Plaintext is a byte array, so convert to string
	return string(response.Plaintext[:])
}

// DryRun is a method used during development for initiating a 'Dry Run'
// of the monzo sweep function.
func dryRun(accessToken string) error {
	config := userConfig{
		accessToken:           accessToken,
		dryRun:                true,
		forceTrigger:          true,
		forceTriggerAmount:    600,
		forceTriggerAccountID: monzoutils.GetFirstAccountIDFromAccessToken(accessToken),
	}

	return processTransaction(config, nil)
}

// MyEvent is a dummy event for AWS Lambda triggering
type MyEvent struct {
	name string
}

// HandleRequest handles the Lambda invocation.  Currently does a dry run.
func HandleRequest(ctx context.Context, event MyEvent) (string, error) {
	accessToken := getAccessToken()

	return "", dryRun(accessToken)
}

func main() {
	lambda.Start(HandleRequest)
}
