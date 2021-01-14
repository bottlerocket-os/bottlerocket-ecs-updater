use crate::args::Args;
use crate::aws::api::Mediator;
use crate::error;
use snafu::ResultExt;
use std::collections::HashMap;
use std::{thread, time};

pub async fn check_updates(args: &Args, aws_api: Box<dyn Mediator>) -> error::Result<()> {
    let list = aws_api
        .list_container_instances(args.cluster_arn.clone())
        .await
        .context(error::ListContainerInstances)?;
    dbg!(list.clone());

    // we need ec2 instance id to send ssm command.
    let instance_details = aws_api
        .describe_container_instances(args.cluster_arn.clone(), &list.container_instance_arns)
        .await
        .context(error::DescribeContainerInstances)?;
    dbg!(instance_details.clone());

    // send ssm command to check for updates
    let params = check_updates_param();
    // TODO: retry on failure
    let ssm_command_details = aws_api
        .send_command(&instance_details.instance_ids, params.to_owned(), Some(120))
        .await
        .context(error::CheckUpdates)?;
    dbg!(ssm_command_details.clone());
    // FIXME : find better way to wait and also retry if command in progress
    thread::sleep(time::Duration::from_secs(5));
    // for each instance check ssm command output
    for instance_id in instance_details.instance_ids {
        let result = aws_api
            .get_command_invocation(ssm_command_details.command_id.clone(), instance_id.clone())
            .await
            .context(error::GetCommandOutput)?;
        dbg!(result);
    }
    Ok(())
}

fn check_updates_param() -> HashMap<String, Vec<String>> {
    let mut params = HashMap::new();
    params.insert("commands".into(), vec!["apiclient update check".into()]);
    params
}
