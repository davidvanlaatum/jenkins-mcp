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
<testsuite name="example.junit" tests="3" failures="2" skipped="0">
  <testcase classname="example.JUnitTest" name="passes"/>
  <testcase classname="example.JUnitTest" name="fails">
    <failure message="intentional fixture failure">expected true but was false</failure>
  </testcase>
  <testcase classname="CalendarRulesTest" name="test should refresh seasonal cutoff date">
    <failure message="seasonal cutoff date mismatch">calendar stack</failure>
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

pipelineJob('example-coverage') {
    description('Buildable pipeline job that publishes coverage results for jenkins-mcp integration tests.')
    definition {
        cps {
            script('''
pipeline {
    agent any
    stages {
        stage('coverage') {
            steps {
                sh """
mkdir -p coverage
cat > coverage/jacoco.xml <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<report name="example">
  <package name="example">
    <class name="example/Covered" sourcefilename="Covered.java">
      <method name="covered" desc="()V" line="1">
        <counter type="INSTRUCTION" missed="0" covered="4"/>
        <counter type="LINE" missed="0" covered="1"/>
        <counter type="METHOD" missed="0" covered="1"/>
      </method>
      <method name="missed" desc="()V" line="2">
        <counter type="INSTRUCTION" missed="3" covered="0"/>
        <counter type="LINE" missed="1" covered="0"/>
        <counter type="METHOD" missed="1" covered="0"/>
      </method>
      <counter type="INSTRUCTION" missed="3" covered="4"/>
      <counter type="LINE" missed="1" covered="1"/>
      <counter type="METHOD" missed="1" covered="1"/>
      <counter type="CLASS" missed="0" covered="1"/>
    </class>
    <sourcefile name="Covered.java">
      <line nr="1" mi="0" ci="4" mb="0" cb="0"/>
      <line nr="2" mi="3" ci="0" mb="0" cb="0"/>
      <counter type="INSTRUCTION" missed="3" covered="4"/>
      <counter type="LINE" missed="1" covered="1"/>
      <counter type="METHOD" missed="1" covered="1"/>
      <counter type="CLASS" missed="0" covered="1"/>
    </sourcefile>
    <counter type="INSTRUCTION" missed="3" covered="4"/>
    <counter type="LINE" missed="1" covered="1"/>
    <counter type="METHOD" missed="1" covered="1"/>
    <counter type="CLASS" missed="0" covered="1"/>
  </package>
  <counter type="INSTRUCTION" missed="3" covered="4"/>
  <counter type="LINE" missed="1" covered="1"/>
  <counter type="METHOD" missed="1" covered="1"/>
  <counter type="CLASS" missed="0" covered="1"/>
</report>
EOF
"""
                recordCoverage tools: [[parser: 'JACOCO', pattern: 'coverage/jacoco.xml']]
            }
        }
    }
}
'''.stripIndent())
            sandbox()
        }
    }
}

freeStyleJob('example-artifacts') {
    description('Buildable freestyle job that publishes text and binary artifacts for jenkins-mcp integration tests.')
    steps {
        shell('''
mkdir -p artifacts/nested
cat > artifacts/report.txt <<'EOF'
hello from artifact fixture
EOF
cat > artifacts/nested/details.txt <<'EOF'
nested artifact fixture
EOF
printf '\\377\\376\\000\\001binary-fixture' > artifacts/blob.bin
'''.stripIndent())
    }
    publishers {
        archiveArtifacts('artifacts/**/*')
    }
}

freeStyleJob('example-watch-lifecycle') {
    description('Quiet-period job used for queue and build watch lifecycle integration tests.')
    quietPeriod(5)
    steps {
        shell('''
echo "starting watch lifecycle fixture"
sleep 2
echo "finished watch lifecycle fixture"
'''.stripIndent())
    }
}

freeStyleJob('example-config-inspection') {
    description('Parameterized freestyle job used for job config inspection integration tests.')
    steps {
        shell('''
echo "fixture-command-secret"
'''.stripIndent())
    }
    configure { project ->
        project / 'properties' / 'hudson.model.ParametersDefinitionProperty' / 'parameterDefinitions' << 'hudson.model.StringParameterDefinition' {
            name('BRANCH')
            defaultValue('main')
            description('Branch to build.')
            trim(false)
        }
        project / 'properties' / 'hudson.model.ParametersDefinitionProperty' / 'parameterDefinitions' << 'hudson.model.StringParameterDefinition' {
            name('DEPLOY_PASSWORD')
            defaultValue('fixture-password-secret')
            description('Deployment password.')
            trim(false)
        }
        project / 'properties' / 'hudson.model.ParametersDefinitionProperty' / 'parameterDefinitions' << 'hudson.model.ChoiceParameterDefinition' {
            name('API_TOKEN')
            description('Token-like choice parameter.')
            choices(class: 'java.util.Arrays$ArrayList') {
                a(class: 'string-array') {
                    string('fixture-choice-secret')
                    string('safe-choice')
                }
            }
        }
        project / 'properties' / 'hudson.model.ParametersDefinitionProperty' / 'parameterDefinitions' << 'hudson.model.StringParameterDefinition' {
            name('REPO_URL')
            defaultValue('https://user:fixture-url-secret@example.com/acme/app.git?token=fixture-query-secret&branch=main')
            description('Repository URL.')
            trim(false)
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
