use crate::{Args, EcsMediator, Instance, SsmInvocationOutput, SsmMediator};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use snafu::{OptionExt, ResultExt, Snafu};
use std::collections::HashMap;

// TODO: might need tuning for better default value
// number of instance to query for check-update in a single ssm command.
const BATCH_INSTANCE_COUNT: i64 = 20;
// time after which ssm command will timeout if not complete
const SSM_CHECK_COMMAND_TIMEOUT_SECS: i64 = 120;

// Used to deserialize check command output
#[derive(Debug, Clone, Deserialize, Serialize, PartialEq)]
struct UpdateInfo {
    chosen_update: Option<Update>,
    update_state: String,
    available_updates: Vec<String>,
}

#[derive(Debug, Clone, Deserialize, Serialize, PartialEq)]
struct Update {
    arch: String,
    version: String,
    variant: String,
}

// Contains all the information required to update instance and track its status
#[derive(Debug, Clone, Deserialize, Serialize, PartialEq)]
pub(crate) struct InstanceInfo {
    instance_id: String,
    instance_status: String,
    update_version: String,
    current_state: InstanceState,
    next_state: InstanceState,
    start_time: Option<DateTime<Utc>>,
    status: UpdateStepStatus,
}

#[derive(Debug, Clone, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "kebab-case")]
pub(crate) enum InstanceState {
    UpdateAvailable,
    Drain,
    Update,
}

#[derive(Debug, Clone, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "kebab-case")]
pub(crate) enum UpdateStepStatus {
    NotStarted,
    Running,
    Success,
    Failed,
}

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

    // Iterates cluster instances in batch and returns instances with updates available
    pub(crate) async fn update_available(&self) -> Result<Vec<InstanceInfo>> {
        // contains token to fetch next set of instances, set to None for 1st batch
        let mut next_token: Option<String> = None;
        let mut instances_to_update: Vec<InstanceInfo> = Vec::new();
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
            let instance_ids = get_instance_ids(&instances.bottlerocket_instances);
            let params = check_updates_param();
            let ssm_command_details = self
                .ssm
                .send_command(
                    instance_ids.clone(),
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

            // iterate instances to get command result and collect instances with available updates
            for instance_id in instance_ids {
                let invocation_result = self
                    .ssm
                    .get_command_invocations(&ssm_command_details.command_id, &instance_id)
                    .await
                    .context(CheckUpdateCommandOutput {
                        command_id: ssm_command_details.command_id.clone(),
                        instance_id: instance_id.clone(),
                    });
                match invocation_result {
                    Ok(output) => {
                        add_if_update_available(&mut instances_to_update, &instance_id, &output)?;
                    }
                    Err(e) => {
                        println!("Error '{}' is not fatal, Ignore instance it will be checked in next iteration!", e)
                    }
                }
            }
            match instances.next_token {
                // Exit the loop if there are no more instances to check
                None => break,
                Some(token) => next_token = Some(token),
            };
        }
        Ok(instances_to_update)
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

// Adds instance `InstanceInfo` to list if it has update available
fn add_if_update_available(
    list: &mut Vec<InstanceInfo>,
    instance_id: &str,
    command_output: &SsmInvocationOutput,
) -> Result<bool> {
    // script should execute successfully
    if command_output.response_code == 0 {
        let update_info = parse_update_status(&command_output.standard_output)?;
        dbg!(update_info.clone());
        // check if update is available
        if update_info.update_state == "Available" {
            list.push(create_instance_info(
                instance_id,
                &command_output.status,
                &update_info,
            )?);
            return Ok(true);
        }
    }
    Ok(false)
}

// Deserializes check update command output
fn parse_update_status(api_output: &str) -> Result<UpdateInfo> {
    Ok(serde_json::from_str(api_output).context(CheckUpdateJson)?)
}

// Creates `InstanceInfo`
fn create_instance_info(
    instance_id: &str,
    instance_status: &str,
    update_info: &UpdateInfo,
) -> Result<InstanceInfo> {
    Ok(InstanceInfo {
        instance_id: instance_id.to_string(),
        instance_status: instance_status.to_string(),
        update_version: update_info
            .chosen_update
            .as_ref()
            .context(self::EmptyChosenUpdate { instance_id })?
            .version
            .clone(),
        current_state: InstanceState::UpdateAvailable,
        next_state: InstanceState::Drain,
        start_time: None,
        status: UpdateStepStatus::NotStarted,
    })
}

type Result<T> = std::result::Result<T, Error>;

/// The error type for this module.
#[derive(Debug, Snafu)]
pub enum Error {
    #[snafu(display("Failed to send check update command: {}", source))]
    CheckUpdateCommand { source: crate::Error },

    #[snafu(display(
        "Failed to get check update command output for command id {} and instance {}: {}",
        command_id,
        instance_id,
        source
    ))]
    CheckUpdateCommandOutput {
        command_id: String,
        instance_id: String,
        source: crate::Error,
    },

    #[snafu(display("Failed to parse check update command output json: {}", source))]
    CheckUpdateJson { source: serde_json::Error },

    #[snafu(display("Failed to get check update command status: {}", source))]
    CommandInvocationStatus { source: crate::Error },

    #[snafu(display(
        "Failed to describe cluster instances to check for updates: {}",
        source
    ))]
    DescribeInstances { source: crate::Error },

    #[snafu(display(
        "Update is available but chosen update is missing for instance {}",
        instance_id
    ))]
    EmptyChosenUpdate { instance_id: String },

    #[snafu(display("Failed to list cluster instances to check for updates: {}", source))]
    ListInstances { source: crate::Error },

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
