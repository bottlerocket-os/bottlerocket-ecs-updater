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

	instances, err := listContainerInstances(*flagCluster, ecsClient)
	errCheck(err)

	bottlerocketInstances, err := describeContainerInstances(*flagCluster, instances, ecsClient)
	errCheck(err)


	fmt.Println(bottlerocketInstances)
}

func errCheck(err error){
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}
}

// checks if ECS Attributes struct contains a specified string
func contains(attrs []*ecs.Attribute, searchString string) bool {
	for _, attr := range attrs {
		if aws.StringValue(attr.Name) == searchString {
			return true
		}
	}
	return false
}

func listContainerInstances(cluster string, ecsClient *ecs.ECS) ([]*string, error) {
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

func describeContainerInstances(cluster string, instances []*string, ecsClient *ecs.ECS) ([]string, error) {
	resp, err := ecsClient.DescribeContainerInstances(&ecs.DescribeContainerInstancesInput{
		Cluster: &cluster, ContainerInstances: instances,
	})
	if err != nil {
		return nil, fmt.Errorf("Cannot describe container instances: %#v", err)
	}
	log.Printf("Container descriptions: %#v", resp)

	var ec2ids []string

	//Check the DescribeInstances response for Bottlerocket nodes, add them to ec2ids if detected
	for _, instance := range resp.ContainerInstances {
		if contains(instance.Attributes, "bottlerocket.variant") {
			ec2ids = append(ec2ids, *instance.Ec2InstanceId)
			log.Printf("Bottlerocket instance detected. Instance %#v added to check updates", *instance.Ec2InstanceId)
		} else {
			log.Printf("No Bottlerocket instances detected")
		}
	}
	return ec2ids, nil
}
