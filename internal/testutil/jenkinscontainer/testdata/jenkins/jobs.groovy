freeStyleJob('example-freestyle') {
    description('Buildable freestyle job created by Job DSL for jenkins-mcp integration tests.')
    steps {
        shell('echo "hello from freestyle"')
    }
}

freeStyleJob('example-junit') {
    description('Buildable freestyle job that publishes JUnit results for jenkins-mcp integration tests.')
    steps {
        shell('''
mkdir -p reports
cat > reports/junit.xml <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="example.junit" tests="2" failures="1" skipped="0">
  <testcase classname="example.JUnitTest" name="passes"/>
  <testcase classname="example.JUnitTest" name="fails">
    <failure message="intentional fixture failure">expected true but was false</failure>
  </testcase>
</testsuite>
EOF
'''.stripIndent())
    }
    publishers {
        archiveJunit('reports/*.xml')
    }
}

freeStyleJob('example-warnings') {
    description('Buildable freestyle job that publishes Warnings NG issues for jenkins-mcp integration tests.')
    steps {
        shell('''
cat > warnings.log <<'EOF'
src/main.c:12:5: warning: example warning from integration fixture [-Wexample]
EOF
'''.stripIndent())
    }
    publishers {
        recordIssues {
            tools {
                gcc {
                    pattern('warnings.log')
                }
            }
        }
    }
}

pipelineJob('example-pipeline') {
    description('Buildable pipeline job created by Job DSL for jenkins-mcp integration tests.')
    definition {
        cps {
            script('''
pipeline {
    agent any
    stages {
        stage('build') {
            steps {
                echo 'hello from pipeline'
            }
        }
    }
}
'''.stripIndent())
            sandbox()
        }
    }
}
