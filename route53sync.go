package main

import (
	"encoding/json"
	"fmt"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/route53"
)

// getMyIp makes a request to myipserver/myip.php on a remote host somewhere.
type MyIpResponse struct {
	IpAddr string `json:"IP"`
}

func getMyIp() (string, error) {

	httpClient := http.Client{
		Timeout: 1 * time.Second,
	}
	resp, err := httpClient.Get("http://abandonedfactory.net/tools/myip.php")
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("error response from remote server: %s", resp.Status)
	}

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	resp.Body.Close()

	var ipStruct MyIpResponse
	err = json.Unmarshal(bodyBytes, &ipStruct)
	if err != nil {
		return "", err
	}

	return ipStruct.IpAddr, nil
}
func doSync() error {
	// Hit the remote IP checker endpoint
	ip, err := getMyIp()
	if err != nil {
		return err
	}

	// Load Hostnames from a config file
	hostnameBytes, err := ioutil.ReadFile("hostnames.csv")
	if err != nil {
		return err
	}
	hostnameLines := strings.Split(string(hostnameBytes), "\n")

	// For each hostname, update Route53 with the IP address
	mySession := session.Must(session.NewSession())
	route53Client := route53.New(mySession)
	changes := make(map[string][]string, 0) // maps hostedZoneId to a set of hostnames to update
	for i := range hostnameLines {
		tokens := strings.Split(hostnameLines[i], ",")
		if len(tokens) != 2 {
			return fmt.Errorf("invalid config line: %s", hostnameLines[i])
		}
		hostedZone := strings.TrimSpace(tokens[0])
		hostName := strings.TrimSpace(tokens[1])

		hostedZoneNames, ok := changes[hostedZone]
		if !ok {
			hostedZoneNames = make([]string, 0)
		}
		hostedZoneNames = append(hostedZoneNames, hostName)
		changes[hostedZone] = hostedZoneNames
	}

	for zone, names := range changes {
		changeSet := []*route53.Change{}
		for i := range names {
			changeSet = append(changeSet, &route53.Change{
				Action: aws.String("UPSERT"),
				ResourceRecordSet: &route53.ResourceRecordSet{
					Name: aws.String(names[i]),
					ResourceRecords: []*route53.ResourceRecord{
						{
							Value: aws.String(ip),
						},
					},
					TTL:  aws.Int64(600),
					Type: aws.String("A"),
				}})
		}
		input := &route53.ChangeResourceRecordSetsInput{
			ChangeBatch: &route53.ChangeBatch{
				Changes: changeSet,
				Comment: aws.String("Web server for klaital.com"),
			},
			HostedZoneId: aws.String(zone),
		}

		result, err := route53Client.ChangeResourceRecordSets(input)
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				case route53.ErrCodeNoSuchHostedZone:
					fmt.Println(route53.ErrCodeNoSuchHostedZone, aerr.Error())
				case route53.ErrCodeNoSuchHealthCheck:
					fmt.Println(route53.ErrCodeNoSuchHealthCheck, aerr.Error())
				case route53.ErrCodeInvalidChangeBatch:
					fmt.Println(route53.ErrCodeInvalidChangeBatch, aerr.Error())
				case route53.ErrCodeInvalidInput:
					fmt.Println(route53.ErrCodeInvalidInput, aerr.Error())
				case route53.ErrCodePriorRequestNotComplete:
					fmt.Println(route53.ErrCodePriorRequestNotComplete, aerr.Error())
				default:
					fmt.Println(aerr.Error())
				}
			} else {
				fmt.Println("Failed to change record sets: %s", err.Error())
			}
		}
		fmt.Printf("%s %s %v", result.String(), zone, names)
	}

	return nil
}

func main() {
	err := doSync()
	if err != nil {
		log.WithError(err).Error("Sync failed")
	}
}
