# 0.2.1

* Bump version of Go to 1.19.1 and upgrade dependencies

# 0.2.0

* Add support for clusters larger than 50 container instances.
* Add after-action summary and done message to log output.
* Add check to reduce the chance of concurrent runs.

Note: In the Bottlerocket ECS updater v0.1.0 release, support for clusters was limited to 50 container instances. In this release, clusters larger than 50 container instances are now supported. :tada: 

# 0.1.0

Initial release of the **Bottlerocket ECS updater** - A service to automatically manage Bottlerocket updates in an Amazon ECS cluster.

The Bottlerocket ECS updater is designed to help you safely automate the routine maintenance of updating the Bottlerocket instances in your cluster.
The updater's safety features include:

* Only tasks that are part of a [service](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/ecs_services.html) will be interrupted.
  Container instances with non-service tasks are skipped for upgrade so no critical workloads will be automatically interrupted.
* Only container instances in the [ACTIVE state](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/container-instance-draining.html) will be upgrade.
  Instances that have been placed into the DRAINING state are skipped for upgrade so other maintenance or debugging can be performed without interruption.

In this first release of the updater, the following considerations should be kept in mind:

* Only clusters of up to 50 container instances are supported.
  If the updater is configured to target a cluster with more than 50 instances, some instances may not be updated.
* When configuring the provided CloudFormation template, ensure that the CloudWatch log group already exists.
  The updater will not automatically create the log group and a missing log group will cause the updater to fail to run.
  When creating a log group, you can configure your desired log retention settings.

See the [README](README.md) for additional information.