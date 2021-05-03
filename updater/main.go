package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/aws/aws-sdk-go/service/ssm"
)

var (
	flagCluster = flag.String("cluster", "", "The short name or full Amazon Resource Name (ARN) of the cluster in which we will manage Bottlerocket instances.")
	flagRegion  = flag.String("region", "", "The AWS Region in which cluster is running.")
)

type updater struct {
	cluster string
	ecs     ECSAPI
	ssm     *ssm.SSM
	ec2     *ec2.EC2
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
		ec2:     ec2.New(sess, aws.NewConfig().WithLogLevel(aws.LogDebugWithHTTPBody)),
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
	for instance := range ec2IDtoECSARN {
		instances = append(instances, instance)
	}

	commandID, err := u.sendCommand(instances, "apiclient update check")
	if err != nil {
		return err
	}

	candidates := make([]string, 0)
	for _, ec2ID := range instances {
		commandOutput, err := u.getCommandResult(commandID, ec2ID)
		if err != nil {
			return err
		}

		updateState, err := isUpdateAvailable(commandOutput)
		if err != nil {
			return err
		}

		if updateState {
			candidates = append(candidates, ec2ID)
		}
	}
	if len(candidates) == 0 {
		log.Printf("No instances to update")
		return nil
	}
	log.Print("Instances ready for update: ", candidates)

	for ec2ID, containerInstance := range ec2IDtoECSARN {
		err := u.drain(containerInstance)
		if err != nil {
			log.Printf("%#v", err)
			continue
		}
		log.Printf("Instance %s drained", ec2ID)

		ec2IDs := []string{ec2ID}
		_, err = u.sendCommand(ec2IDs, "apiclient update apply --reboot")
		if err != nil {
			// TODO add nuanced error checking to determine the type of failure, act accordingly.
			log.Printf("%#v", err)
			err2 := u.activateInstance(aws.String(containerInstance))
			if err2 != nil {
				log.Printf("failed to reactivate %s after failure to execute update command. Aborting update operations.", ec2ID)
				return err2
			}
			continue
		}

		err = u.waitUntilOk(ec2ID)
		if err != nil {
			return fmt.Errorf("instance %s failed to enter an Ok status after reboot. Aborting update operations: %#v", ec2ID, err)
		}

		err = u.activateInstance(aws.String(containerInstance))
		if err != nil {
			log.Printf("instance %s failed to return to ACTIVE after reboot. Aborting update operations.", ec2ID)
			return err
		}

		updateStatus, err := u.sendCommand(ec2IDs, "apiclient update check")
		if err != nil {
			log.Printf("%#v", err)
			continue
		}

		updateResult, err := u.getCommandResult(updateStatus, ec2ID)
		if err != nil {
			log.Printf("%#v", err)
			continue
		}

		// TODO  version before and after comparison.
		updateState, err := isUpdateAvailable(updateResult)
		if err != nil {
			log.Printf("Unable to determine update result. Manual verification of %s required", ec2ID)
			continue
		}

		if updateState {
			log.Printf("Instance %s did not update. Manual update advised.", ec2ID)
			continue
		} else {
			log.Printf("Instance %s updated successfully", ec2ID)
		}

		updatedVersion, err := getActiveVersion(updateResult)
		if err != nil {
			log.Printf("%#v", err)
		}
		if len(updatedVersion) != 0 {
			log.Printf("Instance %s running Bottlerocket: %s", ec2ID, updatedVersion)
		} else {
			log.Printf("Unable to verify active version. Manual verification of %s required.", ec2ID)
		}
	}
	return nil
}
