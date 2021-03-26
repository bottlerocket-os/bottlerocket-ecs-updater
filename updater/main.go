package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecs"
)

var (
	flagCluster = flag.String("cluster", "", "The short name or full Amazon Resource Name (ARN) of the cluster in which we will manage Bottlerocket instances.")
	flagRegion  = flag.String("region", "", "The AWS Region in which cluster is running.")
)

func main() {
	flag.Parse()
	switch {
	case *flagCluster == "":
		log.Println("cluster is required")
		flag.Usage()
		os.Exit(1)
	case *flagRegion == "":
		log.Println("region is required")
		flag.Usage()
		os.Exit(1)
	}

	sess := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(*flagRegion),
	}))
	ecsClient := ecs.New(sess)

	instances, err := listContainerInstances(ecsClient, *flagCluster)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}

	bottlerocketInstances, err := filterBottlerocketInstances(ecsClient, *flagCluster, instances)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}

	if len(bottlerocketInstances) == 0 {
		log.Printf("No Bottlerocket instances detected")
	}
}

func listContainerInstances(ecsClient *ecs.ECS, cluster string) ([]*string, error) {
	resp, err := ecsClient.ListContainerInstances(&ecs.ListContainerInstancesInput{Cluster: &cluster})
	if err != nil {
		return nil, fmt.Errorf("Cannot list container instances: %#v", err)
	}
	log.Printf("%#v", resp)
	var values []*string

	for _, v := range resp.ContainerInstanceArns {
		values = append(values, v)
	}
	return values, nil
}

func filterBottlerocketInstances(ecsClient *ecs.ECS, cluster string, instances []*string) ([]string, error) {
	resp, err := ecsClient.DescribeContainerInstances(&ecs.DescribeContainerInstancesInput{
		Cluster: &cluster, ContainerInstances: instances,
	})
	if err != nil {
		return nil, fmt.Errorf("Cannot describe container instances: %#v", err)
	}
	log.Printf("Container descriptions: %#v", resp)

	var ec2IDs []string

	//Check the DescribeInstances response for Bottlerocket nodes, add them to ec2ids if detected
	for _, instance := range resp.ContainerInstances {
		if containsAttribute(instance.Attributes, "bottlerocket.variant") {
			ec2IDs = append(ec2IDs, *instance.Ec2InstanceId)
			log.Printf("Bottlerocket instance detected. Instance %#v added to check updates", *instance.Ec2InstanceId)
		}
	}
	return ec2IDs, nil
}

// checks if ECS Attributes struct contains a specified string
func containsAttribute(attrs []*ecs.Attribute, searchString string) bool {
	for _, attr := range attrs {
		if aws.StringValue(attr.Name) == searchString {
			return true
		}
	}
	return false
}
