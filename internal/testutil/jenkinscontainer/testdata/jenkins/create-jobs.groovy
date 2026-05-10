import javaposse.jobdsl.dsl.DslScriptLoader
import javaposse.jobdsl.plugin.JenkinsJobManagement
import jenkins.model.Jenkins

def script = new File('/var/jenkins_home/job-dsl/jobs.groovy').text
def workspace = new File(Jenkins.instance.rootDir, 'job-dsl-workspace')
workspace.mkdirs()

def jobManagement = new JenkinsJobManagement(System.out, [:], workspace)
new DslScriptLoader(jobManagement).runScript(script)
