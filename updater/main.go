package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/aws/aws-sdk-go/service/ssm"
)

var (
	flagCluster = flag.String("cluster", "", "The short name or full Amazon Resource Name (ARN) of the cluster in which we will manage Bottlerocket instances.")
	flagRegion  = flag.String("region", "", "The AWS Region in which cluster is running.")
	flagCheck   = flag.String("check-document", "", "The SSM document name for checking available updates.")
	flagApply   = flag.String("apply-document", "", "The SSM document name for applying updates.")
	flagReboot  = flag.String("reboot-document", "", "The SSM document name to initiate a reboot.")
)

const taskDefARNEnv = "TASK_DEFINITION_ARN"

type updater struct {
	cluster        string
	checkDocument  string
	applyDocument  string
	rebootDocument string
	ecs            ECSAPI
	ssm            SSMAPI
	ec2            EC2API
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
	case *flagCheck == "":
		flag.Usage()
		return errors.New("check-document is required")
	case *flagApply == "":
		flag.Usage()
		return errors.New("apply-document is required")
	case *flagReboot == "":
		flag.Usage()
		return errors.New("reboot-document is required")
	}

	sess := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(*flagRegion),
	}))

	u := &updater{
		cluster:        *flagCluster,
		checkDocument:  *flagCheck,
		applyDocument:  *flagApply,
		rebootDocument: *flagReboot,
		ecs:            ecs.New(sess, aws.NewConfig()),
		ssm:            ssm.New(sess, aws.NewConfig()),
		ec2:            ec2.New(sess, aws.NewConfig()),
	}

	family, err := taskDefFamily()
	if err != nil {
		log.Printf("Failed to parse updater task definition arn: %v", err)
		log.Printf("Ignoring check for already running updater")
	} else {
		ok, err := u.alreadyRunning(family)
		if err != nil {
			return fmt.Errorf("Cannot determine running updater tasks, therefore stopping this run to avoid risk of multiple runs: %w", err)
		}
		if ok {
			log.Printf("Another updater is running, therefore exiting this run.")
			return nil
		}
	}

	listedInstances, err := u.listContainerInstances()
	if err != nil {
		return fmt.Errorf("Failed to get container instances in cluster %q: %w", u.cluster, err)
	}
	if len(listedInstances) == 0 {
		log.Print("Zero instances in the cluster")
		return nil
	}

	bottlerocketInstances, err := u.filterBottlerocketInstances(listedInstances)
	if err != nil {
		return fmt.Errorf("Failed to filter Bottlerocket instances: %w", err)
	}

	if len(bottlerocketInstances) == 0 {
		log.Printf("No Bottlerocket instances detected")
		return nil
	}
	candidates, err := u.filterAvailableUpdates(bottlerocketInstances)
	if err != nil {
		return fmt.Errorf("Failed to check updates: %w", err)
	}
	if len(candidates) == 0 {
		log.Printf("No instances to update")
		return nil
	}
	log.Printf("Instances ready for update: %#q", candidates)

	summary := make(map[string]string)
	for _, i := range candidates {
		eligible, err := u.eligible(i.containerInstanceID)
		if err != nil {
			log.Printf("Failed to determine eligibility for update of instance %#q: %v", i, err)
			summary[i.instanceID] = fmt.Sprintf("Failed to determine eligibility for update: %v", err)
			continue
		}
		if !eligible {
			log.Printf("Instance %#q is not eligible for updates because it contains non-service task", i)
			summary[i.instanceID] = "Instance is not eligible for updates because it contains non-service task(s)"
			continue
		}
		log.Printf("Instance %q is eligible for update", i)

		err = u.drainInstance(i.containerInstanceID)
		if err != nil {
			log.Printf("Failed to drain instance %#q: %v", i, err)
			summary[i.instanceID] = fmt.Sprintf("Failed to drain: %v", err)
			continue
		}
		log.Printf("Instance %#q successfully drained!", i)

		updateErr := u.updateInstance(i)
		activateErr := u.activateInstance(i.containerInstanceID)
		if updateErr != nil && activateErr != nil {
			log.Printf("Failed to update instance %#q: %v", i, updateErr)
			return fmt.Errorf("instance %#q failed to re-activate after failing to update: %w", i, activateErr)
		} else if updateErr != nil {
			log.Printf("Failed to update instance %#q: %v", i, updateErr)
			summary[i.instanceID] = fmt.Sprintf("Failed to update: %v", updateErr)
			continue
		} else if activateErr != nil {
			return fmt.Errorf("instance %#q failed to re-activate after update: %w", i, activateErr)
		}

		// Reboots are not immediate, and initiating an SSM command races with reboot. Add some
		// sleep time to allow the reboot to progress before we verify update.
		time.Sleep(20 * time.Second)
		ok, err := u.verifyUpdate(i)
		if err != nil {
			log.Printf("Failed to verify update for instance %#q: %v", i, err)
		}
		if !ok {
			log.Printf("Update failed for instance %#q", i)
			summary[i.instanceID] = "Update failed"
		} else {
			log.Printf("Instance %#q updated successfully!", i)
			summary[i.instanceID] = "Instance updated successfully"
		}
	}
	log.Printf("After action summary:")
	for k, v := range summary {
		log.Printf("%s: %s", k, v)
	}
	log.Printf("Update operations complete!")
	return nil
}

func taskDefFamily() (string, error) {
	taskDefInput := os.Getenv(taskDefARNEnv)
	taskDefARN, err := arn.Parse(taskDefInput)
	if err != nil {
		return "", err
	}
	const taskDefPrefix = "task-definition/"
	if !strings.Contains(taskDefARN.Resource, taskDefPrefix) {
		return "", fmt.Errorf("not a task definition arn: %q", taskDefInput)
	}
	// extract task definition family from resource: task-definition/<task definition family>:<revision>
	taskDef := strings.TrimPrefix(taskDefARN.Resource, taskDefPrefix)
	family := strings.SplitN(taskDef, ":", 2)[0]
	log.Printf("Updater task definition family: %q", family)
	return family, nil
}
