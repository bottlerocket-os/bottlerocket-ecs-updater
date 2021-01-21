use simplelog::LevelFilter;
use structopt::StructOpt;
use url::Url;

/// Bottlerocket ECS Updater Integ
///
/// A test system that deploys Bottlerocket instances into an ECS cluster, deploys the local version
/// of `bottlerocket-ecs-updater` and asserts that it works.
///
#[derive(StructOpt, Debug)]
pub struct IntegArgs {
    /// How much detail to log; from least to most: ERROR, WARN, INFO, DEBUG, TRACE
    #[structopt(long, env = "LOG_LEVEL")]
    pub log_level: LevelFilter,
    /// Complete ECR image name of `bottlerocket-ecs-updater`
    #[structopt(long, env = "BOTTLEROCKET_ECR_UPDATER_IMAGE")]
    pub updater_image: String,
    /// The Region in which cluster is running
    #[structopt(long, env = "BOTTLEROCKET_ECR_REGION")]
    pub region: String,
    /// The Bottlerocket AMI ID to test
    #[structopt(long, env = "BOTTLEROCKET_ECS_AMI_ID")]
    pub ami_id: String,
    // /// The Bottlerocket TUF repository metadata URL
    // #[structopt(long, env = "BOTTLEROCKET_METADATA_URL")]
    // pub metadata_url: Url,
    // /// The Bottlerocket TUF repository targets URL
    // /// #[structopt(long, env = "BOTTLEROCKET_TARGETS_URL")]
    // pub targets_url: Url,
}
