// Command blitzshipz makes read-only calls to the Blitzshipz Open API, to check
// an API key and see what the live answers look like.
//
//	export BLITZSHIPZ_API_KEY=...
//	blitzshipz track CX221367
//	blitzshipz serviceability 110001 400001
//	blitzshipz rates CX123456
//	blitzshipz webhooks
//
// It deliberately cannot create, ship or cancel anything: those cost money or
// summon a courier, and belong in code that records what it did.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/entanglesoftware/blitzshipz/pkg/blitzshipz"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	client, err := blitzshipz.New(blitzshipz.Config{
		APIKey:  os.Getenv("BLITZSHIPZ_API_KEY"),
		BaseURL: os.Getenv("BLITZSHIPZ_BASE_URL"),
	})
	if err != nil {
		return fmt.Errorf("%w (set BLITZSHIPZ_API_KEY)", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch args[0] {
	case "track":
		if len(args) != 2 {
			return usage()
		}
		got, err := client.Orders.Track(ctx, args[1])
		if err != nil {
			return err
		}
		return printRaw(got.Raw)

	case "serviceability":
		if len(args) != 3 {
			return usage()
		}
		from, errFrom := strconv.Atoi(args[1])
		to, errTo := strconv.Atoi(args[2])
		if errFrom != nil || errTo != nil {
			return fmt.Errorf("pincodes are numbers")
		}
		got, err := client.Serviceability.Check(ctx, &blitzshipz.ServiceabilityRequest{
			OriginPincode: from, DestinationPincode: to,
		})
		if err != nil {
			return err
		}
		return printRaw(got.Raw)

	case "rates":
		if len(args) != 2 {
			return usage()
		}
		got, err := client.Rates.Get(ctx, args[1])
		if err != nil {
			return err
		}
		// What Blitzshipz sent, then what the SDK made of it: the two
		// disagreeing is the thing this command exists to show.
		fmt.Println("# Blitzshipz answered:")
		if err := printRaw(got.Raw); err != nil {
			return err
		}
		fmt.Println("# Couriers the SDK read, cheapest first:")
		return printJSON(got.Data.Rates.All())

	case "webhooks":
		got, err := client.Webhooks.List(ctx)
		if err != nil {
			return err
		}
		return printJSON(got)
	}
	return usage()
}

func usage() error {
	return fmt.Errorf("usage: blitzshipz track <awb-or-order-id> | serviceability <from-pincode> <to-pincode> | rates <order-id> | webhooks")
}

func printRaw(raw json.RawMessage) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		_, err := os.Stdout.Write(append(raw, '\n'))
		return err
	}
	return printJSON(v)
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
