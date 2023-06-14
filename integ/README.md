# Integration tests

The following integration workflow is how you can
test your changes and verifying that new dependencies didn’t break the updater mechanisms.
It’s also similar to how we verify versions of the ECS Updater,
so it’s useful to go through it when making changes
and should in total take less than 1 hour.

1. You’ll want to set up a test ECS cluster. 

   Thankfully, this is really easy with the existing integration tests setup script:
   https://github.com/bottlerocket-os/bottlerocket-ecs-updater/blob/develop/integ/setup.sh

   ```sh
   ./setup.sh --ami-id ami-05d2e4a6b8399095a
   ```

   This script expects the ami-id of a Bottlerocket ECS variant.
   This will setup an ECS cluster using the integration CloudFormation stack
   and using that Bottlerocket ECS variant as EC2 compute.

2. Build an ECS updater image from your changes:

   ```
   # Build the image and tag it as "latest"
   make image

   # Verify the image was built and tagged a moment ago
   docker images | head -n 10

   # Re-tag the image to wherever you want to land it on your ECR registry
   docker tag bottlerocket-ecs-updater:latest \
       <account-id>.dkr.ecr.us-west-2.amazonaws.com/bottlerocket-ecs-updater:my-test

   # Push it to your ECR registry
   docker push \
       <account-id>.dkr.ecr.us-west-2.amazonaws.com/bottlerocket-ecs-updater:my-test
   ```

3. Once your integration ECS cluster is up and you’ve built/pushed a new image,
you can execute the run-updater script to actually do the integration tests!

   Note that you need to provide the image URL of the new image you just built.
   This is the actual image that gets deployed as a fargate task!

   ```
   ./run-updater.sh \
       --cluster ecs-updater-integ-cluster \
       --updater-image <account-id>.dkr.ecr.us-west-2.amazonaws.com/bottlerocket-ecs-updater:my-test
   ```

4. Cleanup is also easy! There’s a script for that as well: 

   ```
   ./cleanup.sh --cluster ecs-updater-integ-cluster
   ```

   This tears down the ECS cluster by name releasing any artifacts from the integration tests.

In all, the total process takes well under an hour. ECS clusters spin up and down very quickly.
