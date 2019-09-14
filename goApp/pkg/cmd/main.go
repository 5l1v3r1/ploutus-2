package main

import (
	"encoding/json"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
	// "k8s.io/client-go/rest"
)

// PodMetricsList : PodMetricsList
type PodMetricsList struct {
	Kind       string `json:"kind"`
	APIVersion string `json:"apiVersion"`
	Metadata   struct {
		SelfLink string `json:"selfLink"`
	} `json:"metadata"`
	Items []struct {
		Metadata struct {
			Name              string    `json:"name"`
			Namespace         string    `json:"namespace"`
			SelfLink          string    `json:"selfLink"`
			CreationTimestamp time.Time `json:"creationTimestamp"`
		} `json:"metadata"`
		Timestamp  time.Time `json:"timestamp"`
		Window     string    `json:"window"`
		Containers []struct {
			Name  string `json:"name"`
			Usage struct {
				CPU    string `json:"cpu"`
				Memory string `json:"memory"`
			} `json:"usage"`
		} `json:"containers"`
	} `json:"items"`
}

type CostsList struct {
	Kind       string `json:"kind"`
	APIVersion string `json:"apiVersion"`
	Metadata   struct {
		SelfLink string `json:"selfLink"`
	} `json:"metadata"`
	Items []CostItem `json:"items"`
}
type CostItem struct {
	Kind       string `json:"kind"`
	APIVersion string `json:"apiVersion"`
	Metadata   struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		SelfLink  string `json:"selfLink"`

		ResourceVersion   string    `json:"resourceVersion"`
		CreationTimestamp time.Time `json:"creationTimestamp"`
	} `json:"metadata"`
	Total struct {
		Hour  Data `json:"hour"`
		Day   Data `json:"day"`
		Month Data `json:"month"`
		Year  Data `json:"year"`
	} `json:"total"`
	HistoricData struct {
		Hour  []Data `json:"hour"`
		Day   []Data `json:"day"`
		Month []Data `json:"month"`
		Year  []Data `json:"year"`
	} `json:"historicData"`
	Today time.Time
}
type Data struct {
	Actual  float64 `json:"actual"`
	Request float64 `json:"request"`
	Time    int64   `json:"time"`
}

type PodDetails struct {
	Name         string
	Labels       map[string]string
	Namespace    string
	NodeName     string
	ActualCPU    float64
	RequestedCPU float64
}

type NodeDetails struct {
	Name  string
	Cost  float64
	Cores float64
}

var version string

func getMetrics(clientset *kubernetes.Clientset, nodeDetails map[string]NodeDetails) (map[string]PodDetails, map[string][]PodDetails, map[string][]PodDetails, error) {
	Log("getMetrics")
	var pods PodMetricsList

	podLookUp := make(map[string]PodDetails)
	appLookUp := make(map[string][]PodDetails)
	nodeLookUp := make(map[string][]PodDetails)

	data, err := clientset.RESTClient().Get().AbsPath("apis/metrics.k8s.io/v1beta1/pods").DoRaw()
	if err != nil {
		return podLookUp, appLookUp, nodeLookUp, err
	}
	// fmt.Println( string(data))
	err = json.Unmarshal(data, &pods)
	operationDone := make(map[string]chan bool)
	for _, v := range pods.Items {
		operationDone[v.Metadata.Name+"_"+v.Metadata.Namespace] = make(chan bool)
		func() {
			pd, err := getPods(clientset, v.Metadata.Name, v.Metadata.Namespace)
			if err != nil {
				fmt.Printf("Error retrieving %v in %v \t\t %v \n", v.Metadata.Name, v.Metadata.Namespace, err)
			} else {
				acpu := 0

				for _, vv := range v.Containers {
					if len(vv.Usage.CPU) > 1 {
						vv.Usage.CPU = vv.Usage.CPU[:len(vv.Usage.CPU)-1]
					}
					i, err := strconv.Atoi(vv.Usage.CPU)
					if err != nil {
						fmt.Printf("ERROR ActualCPU \t\t %v \t\t %v\t\t %v\n", v)
					}
					acpu = acpu + i
				}

				//
				cores := nodeDetails[pd.NodeName].Cores
				//Get price e.g. 10 dollar per hour per core on an 8 core vm
				price := nodeDetails[pd.NodeName].Cost / cores
				pd.ActualCPU = float64(acpu) / 1000000000 * price
				pd.RequestedCPU = pd.RequestedCPU * price
				podLookUp[v.Metadata.Name+"-"+pd.Namespace] = pd
				if len(nodeLookUp[pd.NodeName]) < 1 {
					nodeLookUp[pd.NodeName] = make([]PodDetails, 0)
				}
				nodeLookUp[pd.NodeName] = append(nodeLookUp[pd.NodeName], pd)
				if pd.Labels["app"] != "" {
					reg, err := regexp.Compile("[^a-zA-Z0-9]+")
					if err != nil {
						log.Fatal(err)
					}
					appNameNameSpace := strings.ToLower(reg.ReplaceAllString(pd.Labels["app"], "-")) + "_" + pd.Namespace
					if len(appLookUp[appNameNameSpace]) < 1 {
						appLookUp[appNameNameSpace] = make([]PodDetails, 0)
					}
					appLookUp[appNameNameSpace] = append(appLookUp[appNameNameSpace], pd)
				}

			}
			// operationDone[v.Metadata.Name+"_"+v.Metadata.Namespace] <- true
		}()

	}
	// for i, v := range operationDone {
	// 	<-v
	// 	Log("Finished getPods for "+i)
	// }
	return podLookUp, appLookUp, nodeLookUp, err
}
func getPods(clientset *kubernetes.Clientset, name string, namespace string) (PodDetails, error) {
	Log("getPods")
	pod, err := clientset.CoreV1().Pods(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return PodDetails{}, err
	}
	rcpu := int64(0)
	for _, v := range pod.Spec.Containers {

		q := v.Resources.Requests["cpu"]
		r := q.MilliValue()
		j := r
		if err != nil {
			fmt.Printf("ERROR RequestCPU \t\t %v \n", v)
		}
		rcpu = rcpu + j
	}
	return PodDetails{
		Name:         pod.Name,
		Namespace:    pod.Namespace,
		NodeName:     pod.Spec.NodeName,
		Labels:       pod.Labels,
		RequestedCPU: float64(rcpu) / 1000,
	}, err

}

func getNodeDetails(clientset *kubernetes.Clientset) map[string]NodeDetails {
	Log("getNodeDetails")
	toReturn := make(map[string]NodeDetails)
	nodesData, err := clientset.CoreV1().Nodes().List(metav1.ListOptions{})

	if err != nil {
		fmt.Printf("ERROR Getting Nodes \t\t %v \n", err)
	}
	// var nodes NodeList
	// err = json.Unmarshal(nodesData, &nodes)

	for _, v := range nodesData.Items {
		cpu := v.Status.Allocatable["cpu"]
		cpuInt, _ := cpu.AsInt64()
		cpuFloat := float64(cpuInt)
		cost := cpuFloat
		if v.Labels["cost"] != "" {
			cost, err = strconv.ParseFloat(v.Labels["cost"], 64)
			if err != nil {
				fmt.Printf("Error retrieving price, setting price to the no of cores", err)
			}
		}
		toReturn[v.Name] = NodeDetails{
			Name:  v.Name,
			Cost:  cost,
			Cores: cpuFloat,
		}
	}
	return toReturn
}

func main() {
	Log("Starting - Version " + version)
	kubeconfig := "/Users/chris/.kube/config"
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	// config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	nodeDetails := getNodeDetails(clientset)
	podLookUp, appLookUp, nodeLookUp, err := getMetrics(clientset, nodeDetails)

	if err != nil {
		panic(err.Error())
	}
	//
	// nodescostDone := make(chan bool)
	// podscostDone := make(chan bool)
	// appscostDone := make(chan bool)

	// go func() {
	writeNodeData(clientset, nodeLookUp, "nodescost", "NodeCost")
	// nodescostDone <- true
	// }()
	// go func() {
	writePodData(clientset, podLookUp, "podscost", "PodCost")
	// 	podscostDone <- true
	// }()
	// <-nodescostDone
	// <-podscostDone
	// go func() {
	writeAppData(clientset, appLookUp, "appscost", "AppCost")
	// 	appscostDone <- true
	// }()
	//
	// <-appscostDone
}

func writePodData(clientset *kubernetes.Clientset, podLookUp map[string]PodDetails, tpe string, tpe2 string) {
	Log("writePodData")

	d, err := clientset.RESTClient().Get().AbsPath("apis/cminion.com/v1/" + tpe).Namespace("").DoRaw()
	if err != nil {
		panic(err.Error())
	}
	var nodescost CostsList
	err = json.Unmarshal(d, &nodescost)
	nodesMap := make(map[string]CostItem)
	for _, v := range nodescost.Items {
		nodesMap[v.Metadata.Name+"-"+v.Metadata.Namespace] = v
	}
	for i, pod := range podLookUp {
		acpu := 0.0
		rcpu := 0.0
		acpu = acpu + pod.ActualCPU
		rcpu = rcpu + pod.RequestedCPU

		cpu := Data{
			Actual:  acpu,
			Request: rcpu,
			Time:    time.Now().Unix(),
		}
		Log(nodesMap[i].Metadata.Name)
		if nodesMap[i].Metadata.Name != "" {
			nodeItem := nodesMap[i]
			Log(fmt.Sprintf("%v", cpu))
			h := append(nodeItem.HistoricData.Hour, cpu)
			Log(fmt.Sprintf("%v", h))
			nodeItem.HistoricData.Hour = h
			Log(fmt.Sprintf("%v", nodeItem))
			nodeItem.WorkOutAverages()
			Log(fmt.Sprintf("%v", nodeItem))
			json, err := json.Marshal(nodeItem)
			if err != nil {
				panic(err.Error())
			}
			fmt.Println(string(json))
			if b, err := clientset.RESTClient().Put().AbsPath("apis/cminion.com/v1/namespaces/" + pod.Namespace + "/" + tpe).Name(nodeItem.Metadata.Name).Body(json).DoRaw(); err != nil {
				fmt.Println(string(json))
				fmt.Println(string(b))
				panic(err.Error())
			}
		} else {
			newItem := CostItem{}
			newItem.Metadata.Name = pod.Name
			newItem.Metadata.Namespace = pod.Namespace
			newItem.Kind = "PodCost"
			newItem.APIVersion = "cminion.com/v1"
			newItem.HistoricData.Hour = append(newItem.HistoricData.Hour, cpu)
			newItem.Total.Hour = cpu
			newItem.WorkOutAverages()
			json, err := json.Marshal(newItem)
			if err != nil {
				panic(err.Error())
			}
			if b, err := clientset.RESTClient().Post().AbsPath("apis/cminion.com/v1/namespaces/" + pod.Namespace + "/" + tpe).Body(json).DoRaw(); err != nil {
				Log(string(json))
				Log("apis/cminion.com/v1/namespaces/" + pod.Namespace + "/" + tpe)
				fmt.Println(string(b))
				panic(err.Error())
			}
		}
	}
}

func writeNodeData(clientset *kubernetes.Clientset, nodeDetails map[string][]PodDetails, tpe string, tpe2 string) {
	Log("writeNodeData")

	d, err := clientset.RESTClient().Get().AbsPath("apis/cminion.com/v1/" + tpe).DoRaw()
	if err != nil {
		panic(err.Error())
	}
	var nodescost CostsList
	err = json.Unmarshal(d, &nodescost)
	nodesMap := make(map[string]CostItem)
	fmt.Println(nodescost)
	fmt.Println(d)
	for _, v := range nodescost.Items {
		nodesMap[v.Metadata.Name] = v
		fmt.Println(v.Metadata.Name)
	}

	for i, node := range nodeDetails {
		acpu := 0.0
		rcpu := 0.0
		for _, pod := range node {
			acpu = acpu + pod.ActualCPU
			rcpu = rcpu + pod.RequestedCPU
		}
		cpu := Data{
			Actual:  acpu,
			Request: rcpu,
			Time:    time.Now().Unix(),
		}

		if nodesMap[i].Metadata.Name != "" {
			nodeItem := nodesMap[i]
			Log(fmt.Sprintf("%v", cpu))
			h := append(nodeItem.HistoricData.Hour, cpu)
			Log(fmt.Sprintf("%v", h))
			nodeItem.HistoricData.Hour = h
			Log(fmt.Sprintf("%v", nodeItem))
			nodeItem.WorkOutAverages()
			Log(fmt.Sprintf("%v", nodeItem))
			json, err := json.Marshal(nodeItem)
			if err != nil {
				panic(err.Error())
			}
			fmt.Println(string(json))
			if b, err := clientset.RESTClient().Put().AbsPath("apis/cminion.com/v1/" + tpe).Name(i).Body(json).DoRaw(); err != nil {
				fmt.Println(string(json))
				fmt.Println(string(b))
				panic(err.Error())
			}
		} else {
			newItem := CostItem{}
			newItem.Metadata.Name = i
			newItem.Kind = tpe2
			newItem.APIVersion = "cminion.com/v1"
			newItem.HistoricData.Hour = append(newItem.HistoricData.Hour, cpu)
			newItem.Total.Hour = cpu
			newItem.WorkOutAverages()
			json, err := json.Marshal(newItem)
			if err != nil {
				panic(err.Error())
			}
			if b, err := clientset.RESTClient().Post().AbsPath("apis/cminion.com/v1/" + tpe).Body(json).DoRaw(); err != nil {
				fmt.Println(string(json))
				fmt.Println(string(b))

				panic(err.Error())
			}
		}
	}
}

func writeAppData(clientset *kubernetes.Clientset, nodeDetails map[string][]PodDetails, tpe string, tpe2 string) {
	Log("writeAppData")

	d, err := clientset.RESTClient().Get().AbsPath("apis/cminion.com/v1/" + tpe).DoRaw()
	if err != nil {
		panic(err.Error())
	}
	var nodescost CostsList
	err = json.Unmarshal(d, &nodescost)
	nodesMap := make(map[string]CostItem)
	reg, err := regexp.Compile("[^a-zA-Z0-9]+")
	if err != nil {
		log.Fatal(err)
	}

	for _, v := range nodescost.Items {
		key := strings.ToLower(reg.ReplaceAllString(v.Metadata.Name, "-") + "_" + v.Metadata.Namespace)
		nodesMap[key] = v
		fmt.Println(key)
		fmt.Println(key)
	}
	for i, node := range nodeDetails {
		fmt.Println(strings.ToLower(reg.ReplaceAllString(node[0].Labels["app"], "-")))
		acpu := 0.0
		rcpu := 0.0
		for _, pod := range node {
			acpu = acpu + pod.ActualCPU
			rcpu = rcpu + pod.RequestedCPU
		}
		cpu := Data{
			Actual:  acpu,
			Request: rcpu,
			Time:    time.Now().Unix(),
		}
		Log("Name " + nodesMap[i].Metadata.Name)
		Log("i  " + strings.ToLower(reg.ReplaceAllString(node[0].Labels["app"], "-")))
		if nodesMap[i].Metadata.Name != "" {
			nodeItem := nodesMap[i]
			Log(fmt.Sprintf("%v", cpu))
			h := append(nodeItem.HistoricData.Hour, cpu)
			Log(fmt.Sprintf("%v", h))
			nodeItem.HistoricData.Hour = h
			Log(fmt.Sprintf("%v", nodeItem))
			nodeItem.WorkOutAverages()
			Log(fmt.Sprintf("%v", nodeItem))
			json, err := json.Marshal(nodeItem)
			if err != nil {
				panic(err.Error())
			}
			fmt.Println(string(json))
			if b, err := clientset.RESTClient().Put().AbsPath("apis/cminion.com/v1/namespaces/" + nodeItem.Metadata.Namespace + "/" + tpe).Name(strings.ToLower(reg.ReplaceAllString(node[0].Labels["app"], "-"))).Body(json).DoRaw(); err != nil {
				fmt.Println(string(json))
				fmt.Println(string(b))
				panic(err.Error())
			}
		} else {
			newItem := CostItem{}
			newItem.Metadata.Name = strings.ToLower(reg.ReplaceAllString(node[0].Labels["app"], "-"))
			newItem.Kind = tpe2
			newItem.Metadata.Namespace = node[0].Namespace
			newItem.APIVersion = "cminion.com/v1"
			newItem.HistoricData.Hour = append(newItem.HistoricData.Hour, cpu)
			newItem.Total.Hour = cpu
			newItem.WorkOutAverages()
			json, err := json.Marshal(newItem)
			if err != nil {
				panic(err.Error())
			}
			if b, err := clientset.RESTClient().Post().AbsPath("apis/cminion.com/v1/namespaces/" + newItem.Metadata.Namespace + "/" + tpe).Body(json).DoRaw(); err != nil {
				fmt.Println(string(json))
				fmt.Println(string(b))
				fmt.Println(nodesMap[i])
				panic(err.Error())
			}
		}
	}
}

func (this *CostItem) WorkOutAverages() {
	Log("WorkOutAverages for " + this.Metadata.Name)
	//Remove all hour entries over an hour old
	this.Today = time.Now()

	// today.Sub(today.AddDate(0, -1, 0)).Seconds()
	this.Total.Hour = *Average(this.HistoricData.Hour, this.Today.Add(-3.60000288*1000000000000), 60*60)
	this.updateNextInterval("hour")
	this.Total.Day = *Average(this.HistoricData.Day, this.Today.AddDate(0, 0, -1), this.Today.Sub(this.Today.AddDate(0, 0, -1)).Seconds())
	this.updateNextInterval("day")
	this.Total.Month = *Average(this.HistoricData.Month, this.Today.AddDate(0, -1, 0), this.Today.Sub(this.Today.AddDate(0, -1, 0)).Seconds())
	this.updateNextInterval("year")
	this.Total.Year = *Average(this.HistoricData.Year, this.Today.AddDate(-1, 0, 0), this.Today.Sub(this.Today.AddDate(-1, 0, 0)).Seconds())
	this.clean()
}
func (this *CostItem) clean() {
	today := time.Now()
	hourClean := make([]Data, 0)
	for _, v := range this.HistoricData.Hour {
		age := time.Unix(v.Time, 0)
		if age.After(today.Add(-3.60000288 * 1000000000000)) {
			hourClean = append(hourClean, v)
		}
	}
	this.HistoricData.Hour = hourClean

	dayClean := make([]Data, 0)
	for _, v := range this.HistoricData.Day {
		age := time.Unix(v.Time, 0)
		if age.After(today.AddDate(0, 0, -1)) {
			dayClean = append(dayClean, v)
		}
	}
	this.HistoricData.Day = dayClean

	monthClean := make([]Data, 0)
	for _, v := range this.HistoricData.Month {
		age := time.Unix(v.Time, 0)
		if age.After(today.AddDate(0, -1, 0)) {
			monthClean = append(monthClean, v)
		}
	}
	this.HistoricData.Month = monthClean

	yearClean := make([]Data, 0)
	for _, v := range this.HistoricData.Year {
		age := time.Unix(v.Time, 0)
		if age.After(today.AddDate(-1, 0, 0)) {
			yearClean = append(yearClean, v)
		}
	}
	this.HistoricData.Year = yearClean
}
func Average(array []Data, timeCheck time.Time, timeunit float64) *Data {
	Log("Average")
	hAvg := &Data{
		Actual:  0,
		Request: 0,
		Time:    time.Now().Unix(),
	}
	startTime := timeCheck
	for _, v := range array {
		age := time.Unix(v.Time, 0)
		Log("TIME ")
		fmt.Printf("age %v\n", age)
		fmt.Printf("timeCheck %v\n", timeCheck)
		fmt.Printf("delta %v\n", age.Sub(timeCheck))
		if age.After(timeCheck) {
			windowSecs := age.Sub(startTime).Seconds()
			startTime = age
			hAvg.Actual = hAvg.Actual + (v.Actual * windowSecs / timeunit)
			hAvg.Request = hAvg.Request + (v.Request * windowSecs / timeunit)
		}
	}
	Log("*****")
	fmt.Println(array)
	return hAvg
}

func (this *CostItem) updateNextInterval(unit string) {
	Log("updateNextInterval "+unit)
	if (unit == "hour") {
		if time.Unix(this.Total.Day.Time, 0).Before(this.Today.Add(-60*60*1000000000000)) {
			this.HistoricData.Day  = append(this.HistoricData.Day, this.Total.Hour)
		}
	}
	if (unit == "day") {
		if time.Unix(this.Total.Month.Time, 0).Before(this.Today.AddDate(0, 0, -1)) {
			this.HistoricData.Month  = append(this.HistoricData.Month, this.Total.Day)
		}
	}
	if (unit == "month") {
		if time.Unix(this.Total.Year.Time, 0).Before(this.Today.AddDate(0, -1, 0)) {
			this.HistoricData.Year  = append(this.HistoricData.Year, this.Total.Month)
		}
	}
}

func Log(s string) {
	log.Printf("------------------------------\n")
	log.Printf("%v\n", s)
	log.Printf("------------------------------\n")
}
