This will copy your local aws credentials to the container under appuser to allow you to hit the dynamo table.

PowerShell

docker run -p 8080:8080 `
  -e AWS_REGION=us-west-2 `
  -e TABLE_NAME=PDC-Inventory `
  -e AWS_PROFILE=personal `
  -v $HOME\.aws:/home/appuser/.aws:ro `
  ch11-group-project

  Dislaimer, this is for a school project so I understand that there are many unoptimized things.
  realistically front and backend should be seperated, the static deployment codebuild step should filter to just static web files, IAM roles could be better at least privelage.