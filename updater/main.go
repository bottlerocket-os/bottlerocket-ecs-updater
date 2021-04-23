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

type updater struct {
	cluster string
	ecs     *ecs.ECS
	ssm     *ssm.SSM
}

func main() {
	if err := _main(); err != nil {
		log.Println(err.Error())
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

	u := &updater{
		cluster: *flagCluster,
		ecs:     ecs.New(sess, aws.NewConfig().WithLogLevel(aws.LogDebugWithHTTPBody)),
		ssm:     ssm.New(sess, aws.NewConfig().WithLogLevel(aws.LogDebugWithHTTPBody)),
	}

	listedInstances, err := u.listContainerInstances()
	if err != nil {
		return err
	}

	ec2IDtoECSARN, err := u.filterBottlerocketInstances(listedInstances)
	if err != nil {
		return err
	}

	if len(ec2IDtoECSARN) == 0 {
		log.Printf("No Bottlerocket instances detected")
		return nil
	}

	// Make slice of Bottlerocket instances to use with SendCommand and checkCommandOutput
	instances := make([]string, 0)
	for instance, _ := range ec2IDtoECSARN {
		instances = append(instances, instance)
	}

	commandID, err := u.sendCommand(instances, "apiclient update check")
	if err != nil {
		return err
	}

	candidates, err := u.checkSSMCommandOutput(commandID, instances)
	if err != nil {
		return err
	}

	if len(candidates) == 0 {
		log.Printf("No instances to update")
		return nil
	}
	fmt.Println("Instances ready for update: ", candidates)

	for ec2ID, containerInstance := range ec2IDtoECSARN {
		err := u.drain(containerInstance)
		if err != nil {
			log.Printf("%#v", err)
			continue
		}
		log.Printf("Instance %s drained", ec2ID)
	}
	return nil
}
