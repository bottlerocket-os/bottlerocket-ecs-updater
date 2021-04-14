package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/aws/aws-sdk-go/service/ssm"
)

var (
	flagCluster = flag.String("cluster", "", "The short name or full Amazon Resource Name (ARN) of the cluster in which we will manage Bottlerocket instances.")
	flagRegion  = flag.String("region", "", "The AWS Region in which cluster is running.")
)

func main() {
	if err := _main(); err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func _main() error {
	flag.Parse()
	switch {
	case *flagCluster == "":
		flag.Usage()
		return errors.New("cluster is required")
	case *flagRegion == "":
		flag.Usage()
		return errors.New("region is required")
	}

	sess := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(*flagRegion),
	}))
	ecsClient := ecs.New(sess)
	ssmClient := ssm.New(sess, aws.NewConfig().WithLogLevel(aws.LogDebugWithHTTPBody))

	instances, err := listContainerInstances(ecsClient, *flagCluster, pageSize)
	if err != nil {
		return err
	}

	bottlerocketInstances, err := filterBottlerocketInstances(ecsClient, *flagCluster, instances)
	if err != nil {
		return err
	}

	if len(bottlerocketInstances) == 0 {
		log.Printf("No Bottlerocket instances detected")
		return nil
	}

	commandID, err := sendCommand(ssmClient, bottlerocketInstances, "apiclient update check")
	if err != nil {
		return err
	}

	instancesToUpdate, err := checkCommandOutput(ssmClient, commandID, bottlerocketInstances)
	if err != nil {
		return err
	}

	fmt.Println("Instances ready for update: ", instancesToUpdate)
	return nil
}
