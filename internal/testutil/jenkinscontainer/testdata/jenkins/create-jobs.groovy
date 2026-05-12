import javaposse.jobdsl.dsl.DslScriptLoader
import javaposse.jobdsl.plugin.JenkinsJobManagement
import jenkins.model.Jenkins

def script = new File('/var/jenkins_home/job-dsl/jobs.groovy').text
def workspace = new File(Jenkins.instance.rootDir, 'job-dsl-workspace')
workspace.mkdirs()

def jobManagement = new JenkinsJobManagement(System.out, [:], workspace)
new DslScriptLoader(jobManagement).runScript(script)

['example-freestyle', 'example-pipeline', 'example-warnings'].each { jobName ->
    Jenkins.instance.getItemByFullName(jobName)?.scheduleBuild2(0)
}

def artifactJob = Jenkins.instance.getItemByFullName('example-artifacts')
if (artifactJob != null && artifactJob.getLastBuild() == null) {
    artifactJob.scheduleBuild2(0).get()
}
