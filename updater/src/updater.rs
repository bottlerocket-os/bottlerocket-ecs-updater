use crate::{Args, EcsMediator, Instance, SsmMediator};
use snafu::{ResultExt, Snafu};
use std::collections::HashMap;

// TODO: might need tuning for better default value
// number of instance to query for check-update in a single ssm command.
const BATCH_INSTANCE_COUNT: i64 = 20;
// time after which ssm command will timeout if not complete
const SSM_CHECK_COMMAND_TIMEOUT_SECS: i64 = 120;

/// The long-lived object that will watch an ECS cluster and update Bottlerocket hosts.
pub struct Updater<T: EcsMediator, S: SsmMediator> {
    cluster: String,
    ecs: T,
    ssm: S,
}

impl<T: EcsMediator, S: SsmMediator> Updater<T, S> {
    /// Create a new `Updater`.
    pub fn new(args: Args, ecs: T, ssm: S) -> Self {
        Self {
            cluster: args.cluster,
            ecs,
            ssm,
        }
    }

    /// Run the `Updater`
    // TODO - once we start looping we may need a cancellation mechanism, watch for SIGINT etc.
    pub async fn run(&self) -> Result<()> {
        let update_targets = self.update_available().await?;
        if update_targets.is_empty() {
            println!("Zero instances to update!");
            return Ok(());
        }
        // TODO: iterate on instances with available updates to start updates one by one
        Ok(())
    }

    // Iterates cluster instances in batch and returns all instances with updates available
    pub(crate) async fn update_available(&self) -> Result<Vec<String>> {
        // contains token to fetch next set of instances, set to None for 1st batch
        let mut next_token: Option<String> = None;
        loop {
            // get Bottlerocket instances
            let instances = self
                .ecs
                .list_bottlerocket_instances(
                    &self.cluster,
                    Some(BATCH_INSTANCE_COUNT),
                    next_token.clone(),
                )
                .await
                .context(DescribeInstances)?;
            dbg!(instances.clone());

            // send ssm command to check updates
            let params = check_updates_param();
            let ssm_command_details = self
                .ssm
                .send_command(
                    get_instance_ids(&instances.bottlerocket_instances),
                    params,
                    Some(SSM_CHECK_COMMAND_TIMEOUT_SECS),
                )
                .await
                .context(CheckUpdateCommand)?;
            self.ssm
                .wait_command_complete(&ssm_command_details.command_id)
                .await
                .context(WaitCheckUpdateComplete {
                    command_id: ssm_command_details.command_id.clone(),
                })?;

            // get command result
            let _result = self
                .ssm
                .list_command_invocations(&ssm_command_details.command_id, true)
                .await
                .context(CheckUpdateCommandOutput {
                    command_id: &ssm_command_details.command_id,
                })?;

            // TODO parse command output and filter instances with available updates
            match instances.next_token {
                // Exit the loop if there are no more instances to check
                None => break,
                Some(token) => next_token = Some(token),
            };
        }
        // TODO: return instances information with available updates
        Ok(Vec::new())
    }
}

// Gets list of ec2 instance id to check updates
fn get_instance_ids(instances: &[Instance]) -> Vec<String> {
    instances
        .iter()
        .filter_map(|instance| {
            if instance.status == "ACTIVE" || instance.status == "DRAINING" {
                Some(instance.instance_id.clone())
            } else {
                None
            }
        })
        .collect()
}

fn check_updates_param() -> HashMap<String, Vec<String>> {
    let mut params = HashMap::new();
    params.insert("commands".into(), vec!["apiclient update check".into()]);
    params
}

type Result<T> = std::result::Result<T, Error>;

/// The error type for this module.
#[derive(Debug, Snafu)]
pub enum Error {
    #[snafu(display("Failed to list cluster instances to check for updates: {}", source))]
    ListInstances { source: crate::Error },

    #[snafu(display(
        "Failed to describe cluster instances to check for updates: {}",
        source
    ))]
    DescribeInstances { source: crate::Error },

    #[snafu(display("Failed to send check update command: {}", source))]
    CheckUpdateCommand { source: crate::Error },

    #[snafu(display(
        "Failed to get check update command output for command id {}: {}",
        command_id,
        source
    ))]
    CheckUpdateCommandOutput {
        command_id: String,
        source: crate::Error,
    },

    #[snafu(display(
        "Failed to wait for ssm check update command with command_id {} to complete: {}",
        command_id,
        source
    ))]
    WaitCheckUpdateComplete {
        command_id: String,
        source: crate::Error,
    },
}

impl From<Error> for crate::Error {
    fn from(e: Error) -> Self {
        crate::Error::new(e)
    }
}
