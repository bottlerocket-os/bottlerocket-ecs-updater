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
		ecs:     ecs.New(sess, aws.NewConfig()),
		ssm:     ssm.New(sess, aws.NewConfig()),
		ec2:     ec2.New(sess, aws.NewConfig()),
	}

	listedInstances, err := u.listContainerInstances()
	if err != nil {
		return err
	}
	if len(listedInstances) == 0 {
		log.Print("Zero instances in the cluster")
		return nil
	}

	bottlerocketInstances, err := u.filterBottlerocketInstances(listedInstances)
	if err != nil {
		return err
	}

	if len(bottlerocketInstances) == 0 {
		log.Printf("No Bottlerocket instances detected")
		return nil
	}
	candidates, err := u.filterAvailableUpdates(bottlerocketInstances)
	if err != nil {
		return fmt.Errorf("failed to check updates: %#v", err)
	}
	if len(candidates) == 0 {
		log.Printf("No instances to update")
		return nil
	}
	log.Printf("Instances ready for update: %#q", candidates)

	for _, i := range candidates {
		eligible, err := u.eligible(i.containerInstanceID)
		if err != nil {
			log.Printf("Failed to determine eligibility for update of instance %#q: %v", i, err)
			continue
		}
		if !eligible {
			log.Printf("Instance %#q is not eligible for updates", i)
			continue
		}
		err = u.drainInstance(i.containerInstanceID)
		if err != nil {
			log.Printf("Failed to drain instance %#q: %v", i, err)
			continue
		}
		log.Printf("Instance %#q successfully drained!", i)

		ec2IDs := []string{i.instanceID}
		_, err = u.sendCommand(ec2IDs, "apiclient update apply --reboot")
		if err != nil {
			// TODO add nuanced error checking to determine the type of failure, act accordingly.
			log.Printf("%#v", err)
			err2 := u.activateInstance(i.containerInstanceID)
			if err2 != nil {
				log.Printf("failed to re-activate instance %#q after failure to execute update command. Aborting update operations.", i)
				return err2
			}
			continue
		}

		err = u.waitUntilOk(i.instanceID)
		if err != nil {
			return fmt.Errorf("instance %#q failed to enter an Ok status after reboot. Aborting update operations: %#v", i, err)
		}

		err = u.activateInstance(i.containerInstanceID)
		if err != nil {
			log.Printf("instance %#q failed to return to ACTIVE after reboot. Aborting update operations.", i)
			return err
		}

		updateStatus, err := u.sendCommand(ec2IDs, "apiclient update check")
		if err != nil {
			log.Printf("%#v", err)
			continue
		}

		updateResult, err := u.getCommandResult(updateStatus, i.instanceID)
		if err != nil {
			log.Printf("%#v", err)
			continue
		}
		updateState, err := isUpdateAvailable(updateResult)
		if err != nil {
			log.Printf("Unable to determine update result. Manual verification of %#q required", i)
			continue
		}

		if updateState {
			log.Printf("Instance %#q did not update. Manual update advised.", i)
			continue
		} else {
			log.Printf("Instance %#q updated successfully", i)
		}

		updatedVersion, err := getActiveVersion(updateResult)
		if err != nil {
			log.Printf("%#v", err)
		}
		if len(updatedVersion) != 0 {
			log.Printf("Instance %#q running Bottlerocket: %s", i, updatedVersion)
		} else {
			log.Printf("Unable to verify active version. Manual verification of %#q required.", i)
		}
	}
	return nil
}
