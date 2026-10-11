import javaposse.jobdsl.dsl.DslScriptLoader
import javaposse.jobdsl.plugin.JenkinsJobManagement
import jenkins.model.Jenkins

def script = new File('/var/jenkins_home/job-dsl/jobs.groovy').text
def workspace = new File(Jenkins.instance.rootDir, 'job-dsl-workspace')
workspace.mkdirs()

def jobManagement = new JenkinsJobManagement(System.out, [:], workspace)
new DslScriptLoader(jobManagement).runScript(script)

// Empty schedules allow explicit polling without periodic background activity.
['example-freestyle', 'example-pipeline'].each { jobName ->
    def job = Jenkins.instance.getItemByFullName(jobName)
    def trigger = new hudson.triggers.SCMTrigger('')
    job.addTrigger(trigger)
    trigger.start(job, true)
    job.save()
}

def locks = org.jenkins.plugins.lockableresources.LockableResourcesManager.get()
locks.createResourceWithLabel('lab-phone-1', 'lab mobile')
locks.createResourceWithLabel('lab-phone-2', 'lab mobile')
locks.fromName('lab-phone-2').reserve('admin')
locks.save()

['example-freestyle', 'example-pipeline', 'example-junit', 'example-warnings', 'example-coverage', 'example-locks'].each { jobName ->
    Jenkins.instance.getItemByFullName(jobName)?.scheduleBuild2(0)
}

def artifactJob = Jenkins.instance.getItemByFullName('example-artifacts')
if (artifactJob != null && artifactJob.getLastBuild() == null) {
    artifactJob.scheduleBuild2(0).get()
}
