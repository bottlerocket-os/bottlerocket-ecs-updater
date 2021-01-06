use structopt::StructOpt;

/// Bottlerocket ECS Updater Integ
///
/// A test system that deploys Bottlerocket instances into an ECS cluster, deploys the local version
/// of `bottlerocket-ecs-updater` and asserts that it works.
///
#[derive(StructOpt, Debug)]
pub struct IntegArgs {
    /// Complete ECR image name of `bottlerocket-ecs-updater`
    #[structopt(long, env = "UPDATER_IMAGE")]
    pub updater_image: String,
    /// The Region in which cluster is running
    #[structopt(long, env = "REGION")]
    pub region: String,
    /// The Bottlerocket AMI ID to test
    #[structopt(long, env = "AMI_ID")]
    pub ami_id: String,
}
