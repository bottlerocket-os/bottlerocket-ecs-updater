use structopt::StructOpt;

/// Bottlerocket ECS Updater Integ
///
/// A test system that deploys Bottlerocket instances into an ECS cluster, deploys the local version
/// of `bottlerocket-ecs-updater` and asserts that it works.
///
#[derive(StructOpt, Debug)]
pub struct IntegArgs {
    /// The name of the cluster, duh.
    #[structopt(long, env = "BOTTLEROCKET_ECS_UPDATER_CLUSTER_NAME")]
    pub cluster_name: String,
}
