FROM jenkins/jenkins:lts

ENV CASC_JENKINS_CONFIG=/usr/share/jenkins/ref/casc/jenkins.yaml
ENV JAVA_OPTS="-Djenkins.install.runSetupWizard=false"

COPY internal/testutil/jenkinscontainer/testdata/jenkins/plugins.txt /usr/share/jenkins/ref/plugins.txt
RUN jenkins-plugin-cli --plugin-file /usr/share/jenkins/ref/plugins.txt

COPY internal/testutil/jenkinscontainer/testdata/jenkins/jenkins.yaml /usr/share/jenkins/ref/casc/jenkins.yaml
COPY internal/testutil/jenkinscontainer/testdata/jenkins/jobs.groovy /usr/share/jenkins/ref/job-dsl/jobs.groovy
COPY internal/testutil/jenkinscontainer/testdata/jenkins/create-jobs.groovy /usr/share/jenkins/ref/init.groovy.d/create-jobs.groovy
