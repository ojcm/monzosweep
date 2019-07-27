package monzosweep

import (
	"errors"
	"fmt"

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

func formatToGbp(pennies int64) string {
	return fmt.Sprintf("£%v", float64(pennies)/100)
}

func notifyUser(cl monzo.Client, deposits []*monzo.DepositRequest, dryRun bool) error {
	numPots := len(deposits)
	totalDesposited := int64(0)
	for _, deposit := range deposits {
		totalDesposited += deposit.Amount
	}
	fmt.Printf("Total deposits: %s\n", formatToGbp(totalDesposited))

	title := "🛒 Payday Sweep"
	if dryRun {
		title += " (DRY RUN)"
	}

	return cl.CreateFeedItem(&monzo.FeedItem{
		AccountID: deposits[0].AccountID,
		Type:      "basic",
		URL:       "http://www.github.com/ojcm",
		Title:     title,
		Body:      fmt.Sprintf("Transferred %s to %d pots", formatToGbp(totalDesposited), numPots),
		ImageURL:  "https://raw.githubusercontent.com/golang-samples/gopher-vector/master/gopher.png",
	})
}

// TODO implement this
func shouldTriggerSweepFromTransaction(transaction *monzo.Transaction) bool {
	return true
}

func getClient(accessToken string) monzo.Client {
	return monzo.Client{
		BaseURL:     "https://api.monzo.com",
		AccessToken: accessToken,
	}
}

func getAccountWithID(cl monzo.Client, accountID string) (*monzo.Account, error) {

	accounts, err := cl.Accounts("uk_retail")
	if err != nil {
		return nil, err
	}

	var sourceAccount *monzo.Account
	for _, account := range accounts {
		if account.ID == accountID {
			sourceAccount = account
			break
		}
	}
	if sourceAccount == nil {
		return nil, errors.New("Account associated with trigger transaction not found")
	}
	return sourceAccount, nil
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
		fmt.Printf("Creating deposit of %s to pot %s\n", formatToGbp(perPotAmount), pot.Name)
		deposits = append(deposits, &monzo.DepositRequest{
			PotID:          pot.ID,
			AccountID:      sourceAccountID,
			Amount:         perPotAmount,
			IdempotencyKey: getIdempotencyKey(),
		})
	}

	return deposits
}

func getActivePots(cl monzo.Client) ([]*monzo.Pot, error) {
	allPots, err := cl.Pots()
	if err != nil {
		return nil, err
	}
	var activePots []*monzo.Pot
	for _, pot := range allPots {
		if !pot.Deleted {
			activePots = append(activePots, pot)
		}
	}
	if len(activePots) == 0 {
		return nil, errors.New("No active pots found")
	}
	return activePots, nil
}

func processDeposits(cl monzo.Client, deposits []*monzo.DepositRequest) error {
	for _, deposit := range deposits {
		_, err := cl.Deposit(deposit)
		if err != nil {
			return err
		}
	}
	return nil
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
	cl := getClient(config.accessToken)

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
	pots, err := getActivePots(cl)
	if err != nil {
		return err
	}

	// Construct the deposits
	deposits := calcDeposits(sweepAmount, accountID, pots)

	//Send DepositRequests
	if !config.dryRun {
		err = processDeposits(cl, deposits)
		if err != nil {
			return err
		}
	}

	notifyUser(cl, deposits, config.dryRun)

	return nil
}

// DryRun is a method used during development for initiating a 'Dry Run'
// of the monzo sweep function.
func DryRun(accessToken string) error {
	config := userConfig{
		accessToken:           accessToken,
		dryRun:                true,
		forceTrigger:          true,
		forceTriggerAmount:    600,
		forceTriggerAccountID: "acc_00009k4AHcfvBYRqiAD6qP",
	}

	return processTransaction(config, nil)
}

func main() {

	config := userConfig{
		accessToken:           "not_my_access_token",
		dryRun:                true,
		forceTrigger:          true,
		forceTriggerAmount:    6,
		forceTriggerAccountID: "acc_not_my_acc",
	}

	err := processTransaction(config, nil)
	if err != nil {
		fmt.Println(err)
	}
}
