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

	if len(listedInstances) == 0 {
		log.Print("No instances in the cluster")
		return nil
	}

	// Ensure listContainerInstances output is processed in batches of 50 or less as
	// SSM SendCommand is limited to 50 instances at a time.
	batch := 50
	candidates := make([]instance, 0)
	for start := 0; start < len(listedInstances); start += batch {
		stop := start + batch
		if stop > len(listedInstances) {
			stop = len(listedInstances)
		}

		bottlerocketInstances, err := u.filterBottlerocketInstances(listedInstances[start:stop])
		if err != nil {
			return err
		}

		if len(bottlerocketInstances) == 0 {
			log.Printf("No Bottlerocket instances detected")
			return nil
		}
		updateCandidates, err := u.filterAvailableUpdates(bottlerocketInstances)
		if err != nil {
			return fmt.Errorf("failed to check updates: %#v", err)
		}
		candidates = append(candidates, updateCandidates...)
	}
	if len(candidates) == 0 {
		log.Printf("No instances to update")
		return nil
	}
	log.Printf("Instances ready for update: %#q", candidates)

	for _, inst := range candidates {
		ec2ID := inst.instanceID
		containerInstanceID := inst.containerInstanceID
		err := u.drain(containerInstanceID)
		if err != nil {
			log.Printf("%#v", err)
			continue
		}
		log.Printf("Instance %#q drained", inst)

		ec2IDs := []string{ec2ID}
		_, err = u.sendCommand(ec2IDs, "apiclient update apply --reboot")
		if err != nil {
			// TODO add nuanced error checking to determine the type of failure, act accordingly.
			log.Printf("%#v", err)
			err2 := u.activateInstance(aws.String(containerInstanceID))
			if err2 != nil {
				log.Printf("failed to reactivate %#q after failure to execute update command. Aborting update operations.", inst)
				return err2
			}
			continue
		}

		err = u.waitUntilOk(ec2ID)
		if err != nil {
			return fmt.Errorf("instance %#q failed to enter an Ok status after reboot. Aborting update operations: %#v", inst, err)
		}

		err = u.activateInstance(aws.String(containerInstanceID))
		if err != nil {
			log.Printf("instance %#q failed to return to ACTIVE after reboot. Aborting update operations.", inst)
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
			log.Printf("Unable to determine update result. Manual verification of %#q required", inst)
			continue
		}

		if updateState {
			log.Printf("Instance %#q did not update. Manual update advised.", inst)
			continue
		} else {
			log.Printf("Instance %#q updated successfully", inst)
		}

		updatedVersion, err := getActiveVersion(updateResult)
		if err != nil {
			log.Printf("%#v", err)
		}
		if len(updatedVersion) != 0 {
			log.Printf("Instance %#q running Bottlerocket: %s", inst, updatedVersion)
		} else {
			log.Printf("Unable to verify active version. Manual verification of %#q required.", inst)
		}
	}
	return nil
}
