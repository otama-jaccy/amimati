package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type tags []types.Tag

func (t *tags) String() string {
	return fmt.Sprintf("%v", *t)
}

func (t *tags) Set(value string) error {
	for _, tt := range strings.Split(value, ",") {
		var key, val string
		v := strings.Split(tt, ":")
		if len(v) != 2 {
			return fmt.Errorf("invalid tag: %s", tt)
		}
		key = v[0]
		val = v[1]
		*t = append(*t, types.Tag{Key: &key, Value: &val})
	}
	return nil
}

type options struct {
	verbose      bool
	instanceID   string
	imageName    string
	imageTags    tags
	snapshotTags tags
}

func main() {

	var opt options
	flag.BoolVar(&opt.verbose, "v", false, "verbose output")
	flag.StringVar(&opt.instanceID, "instance-id", "", "instance ID")
	flag.StringVar(&opt.imageName, "name", "", "image name")
	flag.Var(&opt.imageTags, "image-tag", "image tags(eg. key1:val1)")
	flag.Var(&opt.snapshotTags, "snapshot-tag", "snapshot tags(eg. key1:val1)")
	flag.Parse()

	if opt.instanceID == "" {
		fmt.Println("instance ID is required")
		os.Exit(1)
	}

	if opt.imageName == "" {
		fmt.Println("image name is required")
		os.Exit(1)
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		fmt.Printf("error loading config: %v\n", err)
		os.Exit(1)
	}

	client := ec2.NewFromConfig(cfg)

	ts := make([]types.TagSpecification, 2)
	if len(opt.imageTags) > 0 {
		ts = append(ts, types.TagSpecification{ResourceType: types.ResourceTypeImage, Tags: opt.imageTags})
	}
	if len(opt.snapshotTags) > 0 {
		ts = append(ts, types.TagSpecification{ResourceType: types.ResourceTypeSnapshot, Tags: opt.snapshotTags})
	}

	createdImageOutput, err := client.CreateImage(ctx, &ec2.CreateImageInput{
		Name:              &opt.imageName,
		InstanceId:        &opt.instanceID,
		TagSpecifications: ts,
	})
	if err != nil {
		fmt.Printf("error creating image: %v\n", err)
		os.Exit(1)
	}

	var snapshotId string
	var createdImage types.Image
	for {
		describeImage, err := client.DescribeImages(ctx, &ec2.DescribeImagesInput{ImageIds: []string{*createdImageOutput.ImageId}})
		if err != nil {
			fmt.Printf("error describing image: %v\n", err)
			os.Exit(1)
		}
		if len(describeImage.Images) == 0 {
			fmt.Println("no images found")
			os.Exit(1)
		}

		if describeImage.Images[0].BlockDeviceMappings[0].Ebs.SnapshotId != nil {
			snapshotId = *describeImage.Images[0].BlockDeviceMappings[0].Ebs.SnapshotId
			createdImage = describeImage.Images[0]
			break
		}

		if opt.verbose {
			fmt.Println("waiting for snapshot to be created")
		}
		time.Sleep(5 * time.Second)
	}

	for {
		snapshotsOutput, err := client.DescribeSnapshots(ctx, &ec2.DescribeSnapshotsInput{SnapshotIds: []string{snapshotId}})
		if err != nil {
			fmt.Printf("error describing snapshots: %v\n", err)
			os.Exit(1)
		}

		if len(snapshotsOutput.Snapshots) == 0 {
			fmt.Println("no snapshots found")
			os.Exit(1)
		}

		snapshot := snapshotsOutput.Snapshots[0]
		if snapshot.State == types.SnapshotStateCompleted {
			break
		} else if snapshot.State == types.SnapshotStateError {
			fmt.Println("snapshot creation failed")
			os.Exit(1)
		} else if snapshot.State != types.SnapshotStatePending {
			fmt.Printf("snapshot state: %v\n", snapshot.State)
			os.Exit(1)
		}

		if opt.verbose {
			fmt.Printf("snapshot state: %v, progress: %s\n", snapshot.State, *snapshot.Progress)
		}
		time.Sleep(5 * time.Second)
	}

	o, err := json.Marshal(createdImage)
	if err != nil {
		fmt.Printf("error marshalling image: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%s\n", o)
}
