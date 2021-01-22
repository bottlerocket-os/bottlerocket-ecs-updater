use crate::args::Args;
use crate::aws::api::Mediator;
use crate::error;
use log::{debug, info};
use snafu::ResultExt;
use std::collections::HashMap;
use std::{thread, time};

pub async fn check_updates(args: &Args, aws_api: Box<dyn Mediator>) -> error::Result<()> {
    info!(
        "Requesting list of container instances for cluster: {}",
        &args.cluster_name
    );
    let list = aws_api
        .list_container_instances(args.cluster_name.clone())
        .await
        .context(error::ListContainerInstances)?;
    debug!("List of container instances: {:?}", &list);

    info!("Requesting list of ec2 instances ids for cluster container instances");
    let instance_details = aws_api
        .describe_container_instances(args.cluster_name.clone(), &list.container_instance_arns)
        .await
        .context(error::DescribeContainerInstances)?;
    debug!("List of instance ids: {:?}", &instance_details);

    let params = check_updates_param();
    // TODO: retry on failure
    info!("Send ssm command to check for updates");
    // debug!("Sending ssm command to check updates on instances: {:?}",);
    let ssm_command_details = aws_api
        .send_command(&instance_details.instance_ids, params.to_owned(), Some(120))
        .await
        .context(error::CheckUpdates)?;
    debug!("ssm command id: {}", &ssm_command_details.command_id);

    thread::sleep(time::Duration::from_millis(2000));
    info!("Get ssm send command result");
    // TODO - eliminate hosts not running Bottlerocket
    // TODO - eliminate hosts that have non-service tasks

    let result = aws_api
        .list_command_invocations(ssm_command_details.command_id.clone())
        .await
        .context(error::GetCommandOutput)?;
    dbg!(result);

    Ok(())
}

fn check_updates_param() -> HashMap<String, Vec<String>> {
    let mut params = HashMap::new();
    params.insert("commands".into(), vec!["apiclient update check".into()]);
    params
}
